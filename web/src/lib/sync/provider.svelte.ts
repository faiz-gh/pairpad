import * as Y from 'yjs';

// Wire message types; see docs/protocol.md.
const MSG_UPDATE = 0x00;
const MSG_SYNCED = 0x01;

const MIN_BACKOFF_MS = 500;
const MAX_BACKOFF_MS = 10_000;

export type SyncStatus = 'connecting' | 'synced' | 'offline';

/**
 * Syncs a Y.Doc with a Pairpad room over a WebSocket.
 *
 * The server is a dumb relay: it replays every stored update, sends SYNCED,
 * then forwards peers' updates. It never decodes Yjs, so all merging happens
 * here.
 */
export class PairpadProvider {
	status = $state<SyncStatus>('connecting');
	/** True once the first full history replay has been applied. */
	hasSynced = $state(false);

	#doc: Y.Doc;
	#url: string;
	#ws: WebSocket | null = null;
	#connected = false; // SYNCED received on the current socket
	#replay: Uint8Array[] = []; // updates received before SYNCED on the current socket
	#attempt = 0;
	#retryTimer: ReturnType<typeof setTimeout> | undefined;
	#destroyed = false;

	constructor(doc: Y.Doc, url: string) {
		this.#doc = doc;
		this.#url = url;
		doc.on('update', this.#onDocUpdate);
		this.#connect();
	}

	destroy() {
		this.#destroyed = true;
		clearTimeout(this.#retryTimer);
		this.#doc.off('update', this.#onDocUpdate);
		this.#ws?.close();
		this.#ws = null;
	}

	#connect() {
		this.status = 'connecting';
		this.#connected = false;
		this.#replay = [];

		const ws = new WebSocket(this.#url);
		ws.binaryType = 'arraybuffer';
		this.#ws = ws;

		ws.onmessage = (event) => {
			if (!(event.data instanceof ArrayBuffer)) return;
			const frame = new Uint8Array(event.data);
			if (frame.length === 0) return;
			switch (frame[0]) {
				case MSG_UPDATE: {
					const update = frame.subarray(1);
					if (!this.#connected) this.#replay.push(update);
					Y.applyUpdate(this.#doc, update, this);
					break;
				}
				case MSG_SYNCED:
					this.#onSynced();
					break;
				// Unknown types are ignored for forward compatibility.
			}
		};

		ws.onclose = () => {
			if (this.#ws !== ws) return;
			this.#ws = null;
			this.#connected = false;
			if (this.#destroyed) return;
			this.status = 'offline';
			const delay = Math.min(MAX_BACKOFF_MS, MIN_BACKOFF_MS * 2 ** this.#attempt++);
			this.#retryTimer = setTimeout(() => this.#connect(), delay);
		};
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
			this.#send(Y.encodeStateAsUpdate(this.#doc, serverState));
		}
	}

	#onDocUpdate = (update: Uint8Array, origin: unknown) => {
		// Remote updates arrive with origin === this and must not be echoed.
		if (origin === this || !this.#connected) return;
		this.#send(update);
	};

	#send(update: Uint8Array) {
		const ws = this.#ws;
		if (!ws || ws.readyState !== WebSocket.OPEN) return;
		const frame = new Uint8Array(update.length + 1);
		frame[0] = MSG_UPDATE;
		frame.set(update, 1);
		ws.send(frame);
	}
}
