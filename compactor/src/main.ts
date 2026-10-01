import pg from 'pg';
import { loadConfig } from './config.ts';
import { compactRoom, findCandidates } from './db.ts';
import { createLogger, type Logger } from './log.ts';

const cfg = loadConfig(process.env);
const log = createLogger(cfg.logLevel);
const pool = new pg.Pool({ connectionString: cfg.databaseUrl, max: 2 });
pool.on('error', (err) => log.warn('idle database connection error', { err: err.message }));

let stopping = false;
let wake: (() => void) | undefined;
for (const signal of ['SIGINT', 'SIGTERM'] as const) {
	process.on(signal, () => {
		log.info('shutting down');
		stopping = true;
		wake?.();
	});
}

async function pass(log: Logger): Promise<void> {
	const rooms = await findCandidates(pool, cfg.minUpdates, cfg.roomsPerPass);
	for (const roomId of rooms) {
		if (stopping) return;
		const started = performance.now();
		try {
			const result = await compactRoom(pool, roomId);
			const ms = Math.round(performance.now() - started);
			if (result.status === 'compacted') {
				log.info('compacted room', { room: roomId, ...result, ms });
			} else {
				log.debug('skipped room', { room: roomId, reason: result.reason });
			}
		} catch (err) {
			// The transaction rolled back, so the room's rows are untouched.
			log.error('compaction failed', { room: roomId, err: String(err) });
		}
	}
}

log.info('compactor started', {
	intervalMs: cfg.intervalMs,
	minUpdates: cfg.minUpdates,
	roomsPerPass: cfg.roomsPerPass
});

while (!stopping) {
	try {
		await pass(log);
	} catch (err) {
		// Usually the database is unreachable or not migrated yet; retry next tick.
		log.warn('compaction pass failed', { err: String(err) });
	}
	if (stopping) break;
	await new Promise<void>((resolve) => {
		const timer = setTimeout(resolve, cfg.intervalMs);
		wake = () => {
			clearTimeout(timer);
			resolve();
		};
	});
}

await pool.end();
log.info('shutdown complete');
