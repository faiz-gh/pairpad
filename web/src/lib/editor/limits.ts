import { EditorState, type Transaction } from '@codemirror/state';

/** Maximum pad size in characters (UTF-16 code units), about 512 KB of text. */
export const MAX_DOC_CHARS = 512 * 1024;

/**
 * Rejects local edits that would grow the document beyond `max` characters.
 * Edits that don't grow it (deletions, replacements) always pass, so an
 * oversized pad can still be trimmed. Remote changes must never be filtered:
 * dropping one would silently diverge this editor from the shared Y.Text.
 *
 * The server can't measure a Yjs document (it never decodes one), so this is
 * the real enforcement; the server only has a coarse backstop on history size.
 */
export function docSizeLimit(options: {
	max: number;
	isRemote: (tr: Transaction) => boolean;
	onReject: () => void;
}) {
	return EditorState.transactionFilter.of((tr) => {
		if (!tr.docChanged || options.isRemote(tr)) return tr;
		const after = tr.newDoc.length;
		if (after <= options.max || after <= tr.startState.doc.length) return tr;
		options.onReject();
		return [];
	});
}
