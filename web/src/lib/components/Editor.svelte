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
	import { yCollab, ySyncAnnotation, yUndoManagerKeymap } from 'y-codemirror.next';
	import { untrack } from 'svelte';
	import type * as Y from 'yjs';
	import type { Awareness } from 'y-protocols/awareness';
	import { loadLanguage, type LanguageId } from '$lib/editor/languages';
	import { MAX_DOC_CHARS, docSizeLimit } from '$lib/editor/limits';
	import { notice } from '$lib/notice.svelte';

	let {
		text,
		awareness,
		language,
		readOnly = false
	}: { text: Y.Text; awareness: Awareness; language: LanguageId; readOnly?: boolean } = $props();

	let host: HTMLDivElement;
	// Reactive so the effects below re-apply to a freshly created view.
	let view = $state.raw<EditorView>();
	const editable = new Compartment();
	const languageSupport = new Compartment();

	// A lean alternative to codemirror's basicSetup, which pulls in
	// autocomplete, lint and search we don't use and would blow the 200 KB
	// bundle budget. Language support is loaded lazily (see languages.ts).
	// Undo/redo comes from yCollab.
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

	// Colors come from CSS variables (routes/layout.css), so light/dark is a
	// class change on <html> and the editor never needs reconfiguring.
	const sizeLimit = docSizeLimit({
		max: MAX_DOC_CHARS,
		// y-codemirror.next tags the transactions it applies from the Y.Text.
		isRemote: (tr) => tr.annotation(ySyncAnnotation) !== undefined,
		onReject: () => notice.show('This pad has reached its 512 KB size limit.')
	});

	const theme = EditorView.theme({
		'&': {
			height: '100%',
			fontSize: '14px',
			backgroundColor: 'var(--background)',
			color: 'var(--foreground)'
		},
		'.cm-scroller': { fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace' },
		'.cm-content': { caretColor: 'var(--foreground)' },
		'.cm-cursor, .cm-dropCursor': { borderLeftColor: 'var(--foreground)' },
		'.cm-gutters': {
			backgroundColor: 'var(--muted)',
			color: 'var(--muted-foreground)',
			borderRight: '1px solid var(--border)'
		},
		'.cm-activeLine': { backgroundColor: 'var(--editor-active-line)' },
		'.cm-activeLineGutter': {
			backgroundColor: 'var(--editor-active-line)',
			color: 'var(--foreground)'
		},
		// The base theme styles the focused selection with an extra scoping
		// class, so match its selector depth for ours to win.
		'.cm-selectionLayer .cm-selectionBackground, &.cm-focused > .cm-scroller > .cm-selectionLayer .cm-selectionBackground':
			{ backgroundColor: 'var(--editor-selection) !important' },
		'.cm-matchingBracket, &.cm-focused .cm-matchingBracket': {
			backgroundColor: 'var(--editor-matching-bracket) !important',
			outline: 'none'
		}
	});

	// The view is created once per Y.Text; readOnly changes are applied by
	// reconfiguring a compartment instead of rebuilding the editor.
	$effect(() => {
		const ytext = text;
		const presence = awareness;
		const v = new EditorView({
			parent: host,
			state: EditorState.create({
				doc: ytext.toString(),
				extensions: [
					setup,
					yCollab(ytext, presence),
					sizeLimit,
					editable.of(EditorView.editable.of(!untrack(() => readOnly))),
					languageSupport.of([]),
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

	// The editor is usable immediately; highlighting arrives once the
	// language's chunk has loaded. A newer choice supersedes a pending load.
	$effect(() => {
		const v = view;
		const id = language;
		if (!v) return;
		let stale = false;
		loadLanguage(id)
			.then((ext) => {
				if (!stale) v.dispatch({ effects: languageSupport.reconfigure(ext) });
			})
			.catch((err) => console.error(`failed to load ${id} support`, err));
		return () => {
			stale = true;
		};
	});
</script>

<div bind:this={host} class="h-full min-h-0"></div>
