import assert from 'node:assert/strict';
import { test } from 'node:test';
import * as Y from 'yjs';
import { parseDuration, loadConfig } from './config.ts';
import { buildSnapshot } from './snapshot.ts';

/** Records every update a doc emits, like the server's room_updates log. */
function recordingDoc() {
	const doc = new Y.Doc();
	const log: Uint8Array[] = [];
	doc.on('update', (u: Uint8Array) => log.push(u));
	return { doc, text: doc.getText('content'), log };
}

function textOf(update: Uint8Array): string {
	const doc = new Y.Doc();
	Y.applyUpdate(doc, update);
	return doc.getText('content').toString();
}

test('snapshot reproduces concurrent edits from several peers', () => {
	const a = recordingDoc();
	const b = recordingDoc();
	for (let i = 0; i < 100; i++) {
		a.text.insert(Math.floor(Math.random() * (a.text.length + 1)), 'a');
		b.text.insert(Math.floor(Math.random() * (b.text.length + 1)), 'b');
	}
	// Peers exchange state, as they would through the relay.
	Y.applyUpdate(a.doc, Y.encodeStateAsUpdate(b.doc));
	Y.applyUpdate(b.doc, Y.encodeStateAsUpdate(a.doc));

	const snapshot = buildSnapshot([...a.log.slice(0, 100), ...b.log.slice(0, 100)]);
	assert.equal(textOf(snapshot), a.text.toString());
	assert.equal(textOf(snapshot), b.text.toString());
});

test('snapshot drops deleted content and is much smaller than the log', () => {
	const { text, log } = recordingDoc();
	for (let i = 0; i < 500; i++) text.insert(text.length, 'x'.repeat(20));
	text.delete(0, text.length - 5);

	const logBytes = log.reduce((n, u) => n + u.length, 0);
	const snapshot = buildSnapshot(log);
	assert.equal(textOf(snapshot), 'xxxxx');
	assert.ok(snapshot.length * 20 < logBytes, `snapshot ${snapshot.length}B vs log ${logBytes}B`);
});

test('snapshot is order-independent and idempotent', () => {
	const { text, log } = recordingDoc();
	for (const ch of 'hello world') text.insert(text.length, ch);

	const shuffled = [...log].reverse();
	const once = buildSnapshot(shuffled);
	// Re-compacting a snapshot plus duplicates of the rows it covers changes nothing.
	const twice = buildSnapshot([once, ...log]);
	assert.equal(textOf(once), 'hello world');
	assert.deepEqual(twice, once);
});

test('compacting a snapshot together with newer updates keeps both', () => {
	const { text, log } = recordingDoc();
	text.insert(0, 'first');
	const snapshot = buildSnapshot(log.splice(0));
	text.insert(text.length, ' second');
	assert.equal(textOf(buildSnapshot([snapshot, ...log])), 'first second');
});

test('updates with missing dependencies are preserved, not dropped', () => {
	const { text, log } = recordingDoc();
	text.insert(0, 'ab');
	text.insert(2, 'cd'); // depends on the first update
	const partial = buildSnapshot([log[1]!]); // only the dependent update
	const full = buildSnapshot([partial, log[0]!]);
	assert.equal(textOf(full), 'abcd');
});

test('malformed updates throw instead of producing a snapshot', () => {
	assert.throws(() => buildSnapshot([new Uint8Array([0xff, 0xff, 0xff, 0xff, 0x01])]));
});

test('parseDuration accepts Go-style units', () => {
	assert.equal(parseDuration('500ms'), 500);
	assert.equal(parseDuration('30s'), 30_000);
	assert.equal(parseDuration('5m'), 300_000);
	assert.throws(() => parseDuration('30'));
	assert.throws(() => parseDuration('1.5s'));
});

test('loadConfig applies defaults and validates', () => {
	const cfg = loadConfig({ DATABASE_URL: 'postgres://x' });
	assert.deepEqual(cfg, {
		databaseUrl: 'postgres://x',
		intervalMs: 30_000,
		minUpdates: 500,
		roomsPerPass: 50,
		logLevel: 'info'
	});
	assert.throws(() => loadConfig({}), /DATABASE_URL/);
	assert.throws(() => loadConfig({ DATABASE_URL: 'x', COMPACT_MIN_UPDATES: '1' }));
	assert.throws(() => loadConfig({ DATABASE_URL: 'x', LOG_LEVEL: 'loud' }));
});
