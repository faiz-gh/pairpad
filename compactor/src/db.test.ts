// Integration tests against a real, migrated Postgres. Skipped unless
// TEST_DATABASE_URL is set, e.g. the compose database:
//   TEST_DATABASE_URL=postgres://pairpad:change-me@localhost:5432/pairpad pnpm test
import assert from 'node:assert/strict';
import { after, before, test } from 'node:test';
import pg from 'pg';
import * as Y from 'yjs';
import { compactRoom, findCandidates } from './db.ts';

const url = process.env.TEST_DATABASE_URL;
const skip = url ? false : 'TEST_DATABASE_URL not set';

let pool: pg.Pool;
const roomIds: string[] = [];

before(async () => {
	if (!url) return;
	pool = new pg.Pool({ connectionString: url });
});

after(async () => {
	if (!url) return;
	await pool.query('DELETE FROM rooms WHERE id = ANY($1)', [roomIds]);
	await pool.end();
});

/** Creates a room whose log holds one row per keystroke of `content`. */
async function seedRoom(content: string): Promise<{ id: string; doc: Y.Doc }> {
	// Test IDs use uppercase, which real room IDs never contain.
	const id = `T${Math.random().toString(36).slice(2, 7).toUpperCase()}`;
	roomIds.push(id);
	await pool.query('INSERT INTO rooms (id) VALUES ($1)', [id]);

	const doc = new Y.Doc();
	const updates: Buffer[] = [];
	doc.on('update', (u: Uint8Array) => updates.push(Buffer.from(u)));
	for (const ch of content) doc.getText('content').insert(doc.getText('content').length, ch);
	for (const u of updates) {
		await pool.query('INSERT INTO room_updates (room_id, data) VALUES ($1, $2)', [id, u]);
	}
	return { id, doc };
}

async function storedText(roomId: string): Promise<{ text: string; rows: number; snapshots: number }> {
	const { rows } = await pool.query<{ data: Buffer; is_snapshot: boolean }>(
		'SELECT data, is_snapshot FROM room_updates WHERE room_id = $1 ORDER BY is_snapshot DESC, id',
		[roomId]
	);
	const doc = new Y.Doc();
	for (const r of rows) Y.applyUpdate(doc, r.data);
	return {
		text: doc.getText('content').toString(),
		rows: rows.length,
		snapshots: rows.filter((r) => r.is_snapshot).length
	};
}

test('compactRoom replaces the log with one snapshot', { skip }, async () => {
	const { id } = await seedRoom('hello compaction');
	assert.ok((await findCandidates(pool, 10, 1000)).includes(id));

	const result = await compactRoom(pool, id);
	assert.equal(result.status, 'compacted');

	assert.deepEqual(await storedText(id), { text: 'hello compaction', rows: 1, snapshots: 1 });
	assert.ok(!(await findCandidates(pool, 2, 1000)).includes(id));
});

test('rows inserted after compaction survive the next pass', { skip }, async () => {
	const { id, doc } = await seedRoom('abc');
	await compactRoom(pool, id);

	const later: Buffer[] = [];
	doc.on('update', (u: Uint8Array) => later.push(Buffer.from(u)));
	doc.getText('content').insert(3, 'def');
	for (const u of later) {
		await pool.query('INSERT INTO room_updates (room_id, data) VALUES ($1, $2)', [id, u]);
	}
	assert.equal((await storedText(id)).text, 'abcdef');

	await compactRoom(pool, id);
	assert.deepEqual(await storedText(id), { text: 'abcdef', rows: 1, snapshots: 1 });
});

test('a malformed row aborts compaction and leaves the log untouched', { skip }, async () => {
	const { id } = await seedRoom('keep me');
	await pool.query('INSERT INTO room_updates (room_id, data) VALUES ($1, $2)', [
		id,
		Buffer.from([0xff, 0xff, 0xff, 0xff, 0x01])
	]);
	const before = (await pool.query('SELECT count(*)::int AS n FROM room_updates WHERE room_id = $1', [id]))
		.rows[0].n;

	await assert.rejects(compactRoom(pool, id));
	const afterCount = (
		await pool.query('SELECT count(*)::int AS n FROM room_updates WHERE room_id = $1', [id])
	).rows[0].n;
	assert.equal(afterCount, before);
});

test('concurrent compactors do not both compact a room', { skip }, async () => {
	const { id } = await seedRoom('race');
	const results = await Promise.all([compactRoom(pool, id), compactRoom(pool, id)]);
	const statuses = results.map((r) => r.status).sort();
	// Either one skipped on the advisory lock, or the second found a single
	// row after the first committed.
	assert.equal(statuses.filter((s) => s === 'compacted').length, 1);
	assert.deepEqual(await storedText(id), { text: 'race', rows: 1, snapshots: 1 });
});
