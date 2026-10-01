import type pg from 'pg';
import { buildSnapshot } from './snapshot.ts';

export type CompactResult =
	| { status: 'compacted'; rows: number; bytesBefore: number; bytesAfter: number }
	| { status: 'skipped'; reason: 'locked' | 'too-few-rows' };

/** Rooms with at least minUpdates stored rows, largest first. */
export async function findCandidates(
	pool: pg.Pool,
	minUpdates: number,
	limit: number
): Promise<string[]> {
	const { rows } = await pool.query<{ room_id: string }>(
		`SELECT room_id FROM room_updates
		 GROUP BY room_id
		 HAVING count(*) >= $1
		 ORDER BY count(*) DESC
		 LIMIT $2`,
		[minUpdates, limit]
	);
	return rows.map((r) => r.room_id);
}

/**
 * Replaces a room's stored updates with one snapshot, in a single transaction.
 *
 * Only the rows read here are deleted (by id), so updates the Go server
 * inserts concurrently are never touched. They simply sit alongside the
 * snapshot until the next pass. A per-room advisory lock keeps two compactor
 * instances from working on the same room at once.
 */
export async function compactRoom(pool: pg.Pool, roomId: string): Promise<CompactResult> {
	const client = await pool.connect();
	try {
		await client.query('BEGIN');

		const lock = await client.query<{ ok: boolean }>(
			'SELECT pg_try_advisory_xact_lock(hashtext($1)) AS ok',
			[`pairpad:compact:${roomId}`]
		);
		if (!lock.rows[0]?.ok) {
			await client.query('ROLLBACK');
			return { status: 'skipped', reason: 'locked' };
		}

		const { rows } = await client.query<{ id: string; data: Buffer }>(
			'SELECT id, data FROM room_updates WHERE room_id = $1 ORDER BY is_snapshot DESC, id',
			[roomId]
		);
		if (rows.length < 2) {
			await client.query('ROLLBACK');
			return { status: 'skipped', reason: 'too-few-rows' };
		}

		const snapshot = buildSnapshot(rows.map((r) => r.data));

		await client.query(
			'INSERT INTO room_updates (room_id, data, is_snapshot) VALUES ($1, $2, true)',
			[roomId, Buffer.from(snapshot)]
		);
		await client.query('DELETE FROM room_updates WHERE room_id = $1 AND id = ANY($2::bigint[])', [
			roomId,
			rows.map((r) => r.id)
		]);
		await client.query('COMMIT');

		return {
			status: 'compacted',
			rows: rows.length,
			bytesBefore: rows.reduce((n, r) => n + r.data.length, 0),
			bytesAfter: snapshot.length
		};
	} catch (err) {
		await client.query('ROLLBACK').catch(() => {});
		throw err;
	} finally {
		client.release();
	}
}
