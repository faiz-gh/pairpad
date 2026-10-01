import { describe, expect, it, vi } from 'vitest';
import { Annotation, EditorState } from '@codemirror/state';
import { docSizeLimit } from './limits';

const remote = Annotation.define<boolean>();

function setup(doc: string, max = 10) {
	const onReject = vi.fn();
	const state = EditorState.create({
		doc,
		extensions: docSizeLimit({ max, isRemote: (tr) => tr.annotation(remote) === true, onReject })
	});
	return { state, onReject };
}

describe('docSizeLimit', () => {
	it('allows edits up to the limit', () => {
		const { state, onReject } = setup('12345');
		const next = state.update({ changes: { from: 5, insert: '67890' } }).state;
		expect(next.doc.toString()).toBe('1234567890');
		expect(onReject).not.toHaveBeenCalled();
	});

	it('rejects a local edit that would exceed the limit', () => {
		const { state, onReject } = setup('1234567890');
		const next = state.update({ changes: { from: 10, insert: 'x' } }).state;
		expect(next.doc.toString()).toBe('1234567890');
		expect(onReject).toHaveBeenCalledOnce();
	});

	it('rejects an oversized paste as a whole', () => {
		const { state } = setup('');
		const next = state.update({ changes: { from: 0, insert: 'x'.repeat(11) } }).state;
		expect(next.doc.length).toBe(0);
	});

	it('always lets remote changes through, even past the limit', () => {
		const { state, onReject } = setup('1234567890');
		const next = state.update({
			changes: { from: 10, insert: 'remote' },
			annotations: remote.of(true)
		}).state;
		expect(next.doc.length).toBe(16);
		expect(onReject).not.toHaveBeenCalled();
	});

	it('lets an oversized document shrink, but not grow', () => {
		const { state } = setup('x'.repeat(20));
		const shrunk = state.update({ changes: { from: 0, to: 5 } }).state;
		expect(shrunk.doc.length).toBe(15);
		const grown = shrunk.update({ changes: { from: 0, insert: 'y' } }).state;
		expect(grown.doc.length).toBe(15);
	});

	it('allows same-length replacements over the limit', () => {
		const { state } = setup('x'.repeat(20));
		const next = state.update({ changes: { from: 0, to: 3, insert: 'abc' } }).state;
		expect(next.doc.toString().startsWith('abc')).toBe(true);
	});
});
