import { describe, expect, it } from 'vitest';
import * as Y from 'yjs';
import { RoomLanguage } from './room-language.svelte';

/** Two docs wired together the way the relay connects peers. */
function peers() {
	const a = new Y.Doc();
	const b = new Y.Doc();
	a.on('update', (u: Uint8Array, origin: unknown) => origin !== 'b' && Y.applyUpdate(b, u, 'a'));
	b.on('update', (u: Uint8Array, origin: unknown) => origin !== 'a' && Y.applyUpdate(a, u, 'b'));
	return { a, b };
}

describe('RoomLanguage', () => {
	it('defaults to plain text', () => {
		expect(new RoomLanguage(new Y.Doc()).current).toBe('plaintext');
	});

	it('syncs a change to other peers', () => {
		const { a, b } = peers();
		const la = new RoomLanguage(a);
		const lb = new RoomLanguage(b);
		la.set('python');
		expect(lb.current).toBe('python');
		lb.set('rust');
		expect(la.current).toBe('rust');
	});

	it('picks up the language from stored history', () => {
		const original = new Y.Doc();
		new RoomLanguage(original).set('go');
		const reloaded = new Y.Doc();
		Y.applyUpdate(reloaded, Y.encodeStateAsUpdate(original));
		expect(new RoomLanguage(reloaded).current).toBe('go');
	});

	it('falls back to plain text for unknown values', () => {
		const doc = new Y.Doc();
		const lang = new RoomLanguage(doc);
		doc.getMap('meta').set('language', 'brainfuck');
		expect(lang.current).toBe('plaintext');
	});

	it('converges when two peers change it concurrently', () => {
		const a = new Y.Doc();
		const b = new Y.Doc();
		const la = new RoomLanguage(a);
		const lb = new RoomLanguage(b);
		la.set('java');
		lb.set('kotlin');
		Y.applyUpdate(b, Y.encodeStateAsUpdate(a));
		Y.applyUpdate(a, Y.encodeStateAsUpdate(b));
		expect(la.current).toBe(lb.current);
	});

	it('stops updating after destroy', () => {
		const doc = new Y.Doc();
		const lang = new RoomLanguage(doc);
		lang.destroy();
		doc.getMap('meta').set('language', 'sql');
		expect(lang.current).toBe('plaintext');
	});
});
