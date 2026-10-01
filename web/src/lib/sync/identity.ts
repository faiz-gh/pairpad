/** How a peer appears to others: cursor label, caret and avatar color. */
export type UserIdentity = {
	name: string;
	/** Caret and avatar color. */
	color: string;
	/** Translucent variant used for selection highlights. */
	colorLight: string;
};

// Distinct, mid-lightness hues that read on both light and dark editor
// backgrounds and keep dark label text legible.
const COLORS = [
	'#30bced',
	'#6eeb83',
	'#ffbc42',
	'#ecd444',
	'#ee6352',
	'#9ac2c9',
	'#8acb88',
	'#fb7185',
	'#c4a1ff',
	'#f59e0b'
];

const ADJECTIVES = [
	'Swift',
	'Quiet',
	'Brave',
	'Clever',
	'Lucky',
	'Sunny',
	'Mellow',
	'Bold',
	'Nimble',
	'Witty'
];
const ANIMALS = [
	'Otter',
	'Falcon',
	'Panda',
	'Lynx',
	'Heron',
	'Koala',
	'Fox',
	'Orca',
	'Gecko',
	'Moose'
];

const STORAGE_KEY = 'pairpad:identity';

function pick<T>(items: readonly T[]): T {
	return items[Math.floor(Math.random() * items.length)]!;
}

function isIdentity(value: unknown): value is UserIdentity {
	const v = value as Partial<UserIdentity> | null;
	return (
		typeof v?.name === 'string' && typeof v.color === 'string' && typeof v.colorLight === 'string'
	);
}

/**
 * Returns this tab's anonymous identity, creating a random one on first
 * use. It is kept in sessionStorage when available: a reload keeps the same
 * name and color, while two tabs still show up as two distinct people. Works
 * without storage too (the identity then lasts for this page only).
 */
export function loadIdentity(): UserIdentity {
	try {
		const stored: unknown = JSON.parse(sessionStorage.getItem(STORAGE_KEY) ?? 'null');
		if (isIdentity(stored)) return stored;
	} catch {
		// Storage unavailable or corrupt: fall through to a fresh identity.
	}
	const color = pick(COLORS);
	const identity = {
		name: `${pick(ADJECTIVES)} ${pick(ANIMALS)}`,
		color,
		colorLight: `${color}33`
	};
	try {
		sessionStorage.setItem(STORAGE_KEY, JSON.stringify(identity));
	} catch {
		// Not persisted; the identity lasts for this page only.
	}
	return identity;
}
