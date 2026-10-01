import type * as Y from 'yjs';
import { DEFAULT_LANGUAGE, isLanguageId, type LanguageId } from '$lib/editor/languages';

const KEY = 'language';

/**
 * The pad's language, stored in the shared Y.Doc (map `meta`, key
 * `language`) so a change reaches every peer and is persisted and compacted
 * with the rest of the document. The server needs no special handling.
 * Concurrent changes converge through Y.Map's last-writer-wins.
 */
export class RoomLanguage {
	current = $state<LanguageId>(DEFAULT_LANGUAGE);

	#meta: Y.Map<unknown>;

	constructor(doc: Y.Doc) {
		this.#meta = doc.getMap('meta');
		this.#meta.observe(this.#sync);
		this.#sync();
	}

	set(id: LanguageId) {
		if (id !== this.current) this.#meta.set(KEY, id);
	}

	destroy() {
		this.#meta.unobserve(this.#sync);
	}

	#sync = () => {
		// Unknown values (e.g. from a newer client) fall back to the default.
		const value = this.#meta.get(KEY);
		this.current = isLanguageId(value) ? value : DEFAULT_LANGUAGE;
	};
}
