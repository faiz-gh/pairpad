import * as Y from 'yjs';

export class SnapshotMismatchError extends Error {}

/**
 * Merges a room's stored updates into a single snapshot update.
 *
 * The updates are applied to a fresh Y.Doc (gc enabled), so content deleted
 * in the pad is dropped rather than carried forward, which is what makes the
 * snapshot smaller than the log it replaces. Updates whose dependencies are
 * missing stay pending in the doc and are still included by
 * encodeStateAsUpdate, so nothing is lost.
 *
 * Throws if an update is malformed or if the snapshot does not reproduce the
 * same document. Callers must then leave the stored rows untouched.
 */
export function buildSnapshot(updates: Uint8Array[]): Uint8Array {
	const doc = new Y.Doc();
	try {
		doc.transact(() => {
			for (const update of updates) Y.applyUpdate(doc, update);
		});
		const snapshot = Y.encodeStateAsUpdate(doc);

		const check = new Y.Doc();
		try {
			Y.applyUpdate(check, snapshot);
			if (!bytesEqual(Y.encodeStateAsUpdate(check), snapshot)) {
				throw new SnapshotMismatchError('snapshot does not reproduce the merged document');
			}
		} finally {
			check.destroy();
		}
		return snapshot;
	} finally {
		doc.destroy();
	}
}

function bytesEqual(a: Uint8Array, b: Uint8Array): boolean {
	if (a.length !== b.length) return false;
	for (let i = 0; i < a.length; i++) if (a[i] !== b[i]) return false;
	return true;
}
