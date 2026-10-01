export type ThemeMode = 'system' | 'light' | 'dark';

/** Keep in sync with the pre-paint script in src/app.html. */
const STORAGE_KEY = 'pairpad:theme';
const DARK_QUERY = '(prefers-color-scheme: dark)';

function readMode(): ThemeMode {
	try {
		const stored = localStorage.getItem(STORAGE_KEY);
		if (stored === 'light' || stored === 'dark') return stored;
	} catch {
		// Storage unavailable (private window, blocked): follow the system.
	}
	return 'system';
}

/**
 * The user's light/dark preference. "system" follows the OS setting live.
 * The resolved mode is applied as the `dark` class on <html>, which is all
 * shadcn's tokens and the editor's CSS variables key off. The choice is a
 * per-browser convenience in localStorage, never shared with the room.
 */
class Theme {
	mode = $state<ThemeMode>(readMode());
	#systemDark = $state(matchMedia(DARK_QUERY).matches);

	isDark = $derived(this.mode === 'dark' || (this.mode === 'system' && this.#systemDark));

	constructor() {
		matchMedia(DARK_QUERY).addEventListener('change', (e) => (this.#systemDark = e.matches));
	}

	set(mode: ThemeMode) {
		this.mode = mode;
		try {
			if (mode === 'system') localStorage.removeItem(STORAGE_KEY);
			else localStorage.setItem(STORAGE_KEY, mode);
		} catch {
			// Not persisted; applies to this page only.
		}
	}

	/** Cycles system → light → dark → system. */
	next(): ThemeMode {
		return this.mode === 'system' ? 'light' : this.mode === 'light' ? 'dark' : 'system';
	}

	apply() {
		document.documentElement.classList.toggle('dark', this.isDark);
	}
}

// Pairpad renders only in the browser (ssr = false), so a module-level
// instance can't leak between requests.
export const theme = new Theme();
