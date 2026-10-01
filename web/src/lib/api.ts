export type Room = {
	id: string;
	createdAt: string;
	lastActiveAt: string;
};

/** Thrown by createRoom when this client has created too many pads recently. */
export class RateLimitedError extends Error {
	constructor(readonly retryAfterSeconds: number) {
		super(`rate limited; retry after ${retryAfterSeconds}s`);
	}
}

export async function createRoom(): Promise<Room> {
	const res = await fetch('/api/rooms', { method: 'POST' });
	if (res.status === 429) {
		throw new RateLimitedError(Number(res.headers.get('Retry-After')) || 60);
	}
	if (!res.ok) throw new Error(`create room failed: ${res.status}`);
	return res.json();
}

/** A user-facing explanation for a createRoom failure. */
export function createRoomErrorMessage(err: unknown): string {
	if (err instanceof RateLimitedError) {
		const s = err.retryAfterSeconds;
		const [n, unit] = s < 60 ? [s, 'second'] : [Math.ceil(s / 60), 'minute'];
		const wait = `${n} ${unit}${n === 1 ? '' : 's'}`;
		return `You're creating pads too quickly. Try again in ${wait}.`;
	}
	return "Couldn't create a pad. Check your connection and try again.";
}

/** Returns null when the room does not exist. */
export async function getRoom(id: string): Promise<Room | null> {
	const res = await fetch(`/api/rooms/${encodeURIComponent(id)}`);
	if (res.status === 404) return null;
	if (!res.ok) throw new Error(`get room failed: ${res.status}`);
	return res.json();
}

export function roomSocketUrl(id: string): string {
	const scheme = location.protocol === 'https:' ? 'wss' : 'ws';
	return `${scheme}://${location.host}/ws/${encodeURIComponent(id)}`;
}
