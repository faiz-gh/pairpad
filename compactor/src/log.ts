import type { LogLevel } from './config.ts';

const RANK: Record<LogLevel, number> = { debug: 0, info: 1, warn: 2, error: 3 };

export type Logger = Record<LogLevel, (msg: string, attrs?: Record<string, unknown>) => void>;

/** JSON-lines logger shaped like the Go server's slog output. */
export function createLogger(level: LogLevel): Logger {
	const emit = (l: LogLevel) => (msg: string, attrs: Record<string, unknown> = {}) => {
		if (RANK[l] < RANK[level]) return;
		console.log(
			JSON.stringify({ time: new Date().toISOString(), level: l.toUpperCase(), msg, ...attrs })
		);
	};
	return { debug: emit('debug'), info: emit('info'), warn: emit('warn'), error: emit('error') };
}
