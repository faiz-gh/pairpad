export type LogLevel = 'debug' | 'info' | 'warn' | 'error';

export type Config = {
	databaseUrl: string; // DATABASE_URL, required
	intervalMs: number; // COMPACT_INTERVAL, default 30s
	minUpdates: number; // COMPACT_MIN_UPDATES, default 500
	roomsPerPass: number; // COMPACT_ROOMS_PER_PASS, default 50
	logLevel: LogLevel; // LOG_LEVEL, default info
};

const UNIT_MS: Record<string, number> = { ms: 1, s: 1000, m: 60_000, h: 3_600_000 };

/** Parses Go-style single-unit durations such as "500ms", "30s" or "5m". */
export function parseDuration(value: string): number {
	const match = /^(\d+)(ms|s|m|h)$/.exec(value.trim());
	if (!match) throw new Error(`invalid duration ${JSON.stringify(value)}`);
	return Number(match[1]) * UNIT_MS[match[2]!]!;
}

export function loadConfig(env: NodeJS.ProcessEnv): Config {
	const databaseUrl = env.DATABASE_URL;
	if (!databaseUrl) throw new Error('DATABASE_URL is required');

	const intervalMs = parseDuration(env.COMPACT_INTERVAL || '30s');
	if (intervalMs <= 0) throw new Error('COMPACT_INTERVAL must be positive');

	const logLevel = (env.LOG_LEVEL || 'info').toLowerCase();
	if (!['debug', 'info', 'warn', 'error'].includes(logLevel)) {
		throw new Error('LOG_LEVEL must be one of debug, info, warn, error');
	}

	return {
		databaseUrl,
		intervalMs,
		// Two rows is the smallest log that compaction can shrink.
		minUpdates: positiveInt(env, 'COMPACT_MIN_UPDATES', 500, 2),
		roomsPerPass: positiveInt(env, 'COMPACT_ROOMS_PER_PASS', 50, 1),
		logLevel: logLevel as LogLevel
	};
}

function positiveInt(env: NodeJS.ProcessEnv, key: string, def: number, min: number): number {
	const raw = env[key];
	if (!raw) return def;
	const n = Number(raw);
	if (!Number.isInteger(n) || n < min) throw new Error(`${key} must be an integer >= ${min}`);
	return n;
}
