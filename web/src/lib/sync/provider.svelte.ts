import * as Y from 'yjs';
import {
	Awareness,
	applyAwarenessUpdate,
	encodeAwarenessUpdate,
	removeAwarenessStates
} from 'y-protocols/awareness';
import * as decoding from 'lib0/decoding';
import type { UserIdentity } from './identity';

// Wire message types; see docs/protocol.md.
const MSG_UPDATE = 0x00;
const MSG_SYNCED = 0x01;
const MSG_AWARENESS = 0x02;
const MSG_PEER_LEFT = 0x03;

// Application close codes; see docs/protocol.md.
const CLOSE_ROOM_FULL = 4008;

const MIN_BACKOFF_MS = 500;
const MAX_BACKOFF_MS = 10_000;
const ROOM_FULL_RETRY_MS = 5_000;

/**
 * `missing`: the room no longer exists (expired); the provider stops retrying.
 * `full`: the room is at its peer limit; the provider keeps retrying.
 */
export type SyncStatus = 'connecting' | 'synced' | 'offline' | 'missing' | 'full';

export type Peer = UserIdentity & { clientId: number; self: boolean };

type AwarenessChanges = { added: number[]; updated: number[]; removed: number[] };

/**
 * Syncs a Y.Doc and its presence (awareness) with a Pairpad room over a
 * WebSocket.
 *
 * The server is a dumb relay: it replays every stored update, sends SYNCED,
 * then forwards peers' updates and awareness frames. It never decodes Yjs, so
 * all merging happens here.
 */
export class PairpadProvider {
	status = $state<SyncStatus>('connecting');
	/** True once the first full history replay has been applied. */
	hasSynced = $state(false);
	/** Everyone in the room, this tab first. */
	peers = $state.raw<Peer[]>([]);

	readonly awareness: Awareness;

	#doc: Y.Doc;
	#url: string;
	#ws: WebSocket | null = null;
	#connected = false; // SYNCED received on the current socket
	#replay: Uint8Array[] = []; // updates received before SYNCED on the current socket
	#attempt = 0;
	#retryTimer: ReturnType<typeof setTimeout> | undefined;
	#destroyed = false;
	#roomExists: () => Promise<boolean>;

	/**
	 * @param roomExists called when a connection attempt fails before opening.
	 *   The server answers the upgrade with 404 for a room that expired, which
	 *   the WebSocket API can't expose, so the provider asks the REST API
	 *   before retrying.
	 */
	constructor(doc: Y.Doc, url: string, user: UserIdentity, roomExists: () => Promise<boolean>) {
		this.#doc = doc;
		this.#url = url;
		this.#roomExists = roomExists;
		this.awareness = new Awareness(doc);
		this.awareness.setLocalStateField('user', user);
		doc.on('update', this.#onDocUpdate);
		this.awareness.on('update', this.#onAwarenessUpdate);
		this.awareness.on('change', this.#refreshPeers);
		this.#refreshPeers();
		this.#connect();
	}

	destroy() {
		this.#destroyed = true;
		clearTimeout(this.#retryTimer);
		this.#doc.off('update', this.#onDocUpdate);
		this.awareness.off('update', this.#onAwarenessUpdate);
		this.awareness.off('change', this.#refreshPeers);
		// The server tells the others we left (PEER_LEFT) when the socket closes.
		this.#ws?.close();
		this.#ws = null;
		this.awareness.destroy();
	}

	#connect() {
		// Keep showing "full" while waiting for a free spot.
		if (this.status !== 'full') this.status = 'connecting';
		this.#connected = false;
		this.#replay = [];

		const ws = new WebSocket(this.#url);
		ws.binaryType = 'arraybuffer';
		this.#ws = ws;
		let opened = false;
		ws.onopen = () => (opened = true);

		ws.onmessage = (event) => {
			if (!(event.data instanceof ArrayBuffer)) return;
			const frame = new Uint8Array(event.data);
			if (frame.length === 0) return;
			const payload = frame.subarray(1);
			switch (frame[0]) {
				case MSG_UPDATE:
					if (!this.#connected) this.#replay.push(payload);
					Y.applyUpdate(this.#doc, payload, this);
					break;
				case MSG_SYNCED:
					this.#onSynced();
					break;
				case MSG_AWARENESS:
					applyAwarenessUpdate(this.awareness, payload, this);
					break;
				case MSG_PEER_LEFT:
					this.#removeRemote(awarenessClientIds(payload));
					break;
				// Unknown types are ignored for forward compatibility.
			}
		};

		ws.onclose = (event) => {
			if (this.#ws !== ws) return;
			this.#ws = null;
			this.#connected = false;
			// Without a server we can't tell who is still here.
			this.#removeRemote([...this.awareness.getStates().keys()]);
			if (this.#destroyed) return;
			if (event.code === CLOSE_ROOM_FULL) {
				this.status = 'full';
				this.#retryTimer = setTimeout(() => this.#connect(), ROOM_FULL_RETRY_MS);
				return;
			}
			this.status = 'offline';
			if (opened) {
				this.#scheduleReconnect();
				return;
			}
			// Rejected before opening: expired room, or just unreachable?
			this.#roomExists()
				.then((exists) => {
					if (this.#destroyed) return;
					if (exists) this.#scheduleReconnect();
					else this.status = 'missing';
				})
				.catch(() => !this.#destroyed && this.#scheduleReconnect());
		};
	}

	#scheduleReconnect() {
		const delay = Math.min(MAX_BACKOFF_MS, MIN_BACKOFF_MS * 2 ** this.#attempt++);
		this.#retryTimer = setTimeout(() => this.#connect(), delay);
	}

	#onSynced() {
		const reconnect = this.hasSynced;
		const serverHistory = this.#replay;
		this.#replay = [];
		this.#connected = true;
		this.#attempt = 0;
		this.hasSynced = true;
		this.status = 'synced';

		// After a reconnect we may hold edits the server never saw (made
		// offline, or in flight when the socket dropped). The replay is the
		// server's entire state, so send only what it is missing.
		if (reconnect) {
			const serverState = Y.encodeStateVectorFromUpdate(Y.mergeUpdates(serverHistory));
			this.#send(MSG_UPDATE, Y.encodeStateAsUpdate(this.#doc, serverState));
		}

		// Announce ourselves. Re-setting the state bumps its clock, which peers
		// need in order to accept it after they removed us on a PEER_LEFT.
		this.awareness.setLocalState(this.awareness.getLocalState());
	}

	#onDocUpdate = (update: Uint8Array, origin: unknown) => {
		// Remote updates arrive with origin === this and must not be echoed.
		if (origin === this || !this.#connected) return;
		this.#send(MSG_UPDATE, update);
	};

	#onAwarenessUpdate = ({ added, updated, removed }: AwarenessChanges, origin: unknown) => {
		if (origin === this || !this.#connected) return;
		// Only ever publish our own state. The server keeps our latest frame
		// and echoes it as PEER_LEFT when we disconnect, so it must not list
		// other clients (e.g. ones our awareness timed out locally).
		const self = this.#doc.clientID;
		if (![...added, ...updated, ...removed].includes(self)) return;
		this.#send(MSG_AWARENESS, encodeAwarenessUpdate(this.awareness, [self]));
	};

	#removeRemote(clientIds: number[]) {
		const self = this.#doc.clientID;
		const remote = clientIds.filter((id) => id !== self);
		if (remote.length > 0) removeAwarenessStates(this.awareness, remote, this);
	}

	#refreshPeers = () => {
		const self = this.#doc.clientID;
		const peers: Peer[] = [];
		for (const [clientId, state] of this.awareness.getStates()) {
			const user = (state as { user?: UserIdentity }).user;
			if (user) peers.push({ ...user, clientId, self: clientId === self });
		}
		peers.sort((a, b) => Number(b.self) - Number(a.self) || a.name.localeCompare(b.name));
		this.peers = peers;
	};

	#send(type: number, payload: Uint8Array) {
		const ws = this.#ws;
		if (!ws || ws.readyState !== WebSocket.OPEN) return;
		const frame = new Uint8Array(payload.length + 1);
		frame[0] = type;
		frame.set(payload, 1);
		ws.send(frame);
	}
}

/**
 * Reads the client IDs from a y-protocols awareness update:
 * varUint(count), then per entry varUint(clientID), varUint(clock), varString(state).
 */
export function awarenessClientIds(update: Uint8Array): number[] {
	try {
		const decoder = decoding.createDecoder(update);
		const count = decoding.readVarUint(decoder);
		const ids: number[] = [];
		for (let i = 0; i < count; i++) {
			ids.push(decoding.readVarUint(decoder));
			decoding.readVarUint(decoder);
			decoding.readVarString(decoder);
		}
		return ids;
	} catch {
		return [];
	}
}
