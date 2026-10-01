/**
 * A single transient message shown in the toolbar (e.g. "Pad size limit
 * reached"). Repeated calls replace the text and restart the timer, so a
 * burst of rejected keystrokes shows one steady notice.
 */
class Notice {
	text = $state<string | null>(null);
	#timer: ReturnType<typeof setTimeout> | undefined;

	show(text: string, ms = 5000) {
		this.text = text;
		clearTimeout(this.#timer);
		this.#timer = setTimeout(() => (this.text = null), ms);
	}

	clear() {
		clearTimeout(this.#timer);
		this.text = null;
	}
}

// Pairpad renders only in the browser (ssr = false), so a module-level
// instance can't leak between requests.
export const notice = new Notice();
