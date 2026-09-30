export type Room = {
	id: string;
	createdAt: string;
	lastActiveAt: string;
};

export async function createRoom(): Promise<Room> {
	const res = await fetch('/api/rooms', { method: 'POST' });
	if (!res.ok) throw new Error(`create room failed: ${res.status}`);
	return res.json();
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
