<script lang="ts">
	import { defaultKeymap, indentWithTab } from '@codemirror/commands';
	import { Compartment, EditorState } from '@codemirror/state';
	import {
		EditorView,
		drawSelection,
		dropCursor,
		highlightActiveLine,
		highlightActiveLineGutter,
		highlightSpecialChars,
		keymap,
		lineNumbers,
		rectangularSelection
	} from '@codemirror/view';
	import { yCollab, yUndoManagerKeymap } from 'y-codemirror.next';
	import { untrack } from 'svelte';
	import type * as Y from 'yjs';

	let { text, readOnly = false }: { text: Y.Text; readOnly?: boolean } = $props();

	let host: HTMLDivElement;
	let view: EditorView | undefined;
	const editable = new Compartment();

	// A lean alternative to codemirror's basicSetup, which pulls in
	// autocomplete, lint, search and language support we don't use yet and
	// would blow the 200 KB bundle budget. Undo/redo comes from yCollab.
	const setup = [
		lineNumbers(),
		highlightActiveLineGutter(),
		highlightSpecialChars(),
		drawSelection(),
		dropCursor(),
		rectangularSelection(),
		highlightActiveLine(),
		EditorState.allowMultipleSelections.of(true),
		keymap.of([...yUndoManagerKeymap, ...defaultKeymap, indentWithTab])
	];

	const theme = EditorView.theme({
		'&': { height: '100%', fontSize: '14px' },
		'.cm-scroller': { fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace' }
	});

	// The view is created once per Y.Text; readOnly changes are applied by
	// reconfiguring a compartment instead of rebuilding the editor.
	$effect(() => {
		const ytext = text;
		const v = new EditorView({
			parent: host,
			state: EditorState.create({
				doc: ytext.toString(),
				extensions: [
					setup,
					yCollab(ytext, null),
					editable.of(EditorView.editable.of(!untrack(() => readOnly))),
					theme
				]
			})
		});
		view = v;
		return () => {
			v.destroy();
			view = undefined;
		};
	});

	$effect(() => {
		const isEditable = !readOnly;
		view?.dispatch({ effects: editable.reconfigure(EditorView.editable.of(isEditable)) });
	});
</script>

<div bind:this={host} class="h-full min-h-0"></div>
