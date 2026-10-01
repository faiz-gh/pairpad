import type { Extension } from '@codemirror/state';
import type { StreamParser } from '@codemirror/language';

/**
 * Languages offered by the picker. Every entry's support is fetched on demand
 * with a dynamic import, so each language (and the shared highlighting code)
 * is its own chunk and none of it counts against the initial bundle.
 */
export const LANGUAGES = [
	{ id: 'plaintext', label: 'Plain text' },
	{ id: 'c', label: 'C' },
	{ id: 'cpp', label: 'C++' },
	{ id: 'csharp', label: 'C#' },
	{ id: 'css', label: 'CSS' },
	{ id: 'go', label: 'Go' },
	{ id: 'html', label: 'HTML' },
	{ id: 'java', label: 'Java' },
	{ id: 'javascript', label: 'JavaScript' },
	{ id: 'json', label: 'JSON' },
	{ id: 'kotlin', label: 'Kotlin' },
	{ id: 'markdown', label: 'Markdown' },
	{ id: 'php', label: 'PHP' },
	{ id: 'python', label: 'Python' },
	{ id: 'ruby', label: 'Ruby' },
	{ id: 'rust', label: 'Rust' },
	{ id: 'shell', label: 'Shell' },
	{ id: 'sql', label: 'SQL' },
	{ id: 'swift', label: 'Swift' },
	{ id: 'typescript', label: 'TypeScript' },
	{ id: 'yaml', label: 'YAML' }
] as const;

export type LanguageId = (typeof LANGUAGES)[number]['id'];

export const DEFAULT_LANGUAGE: LanguageId = 'plaintext';

const IDS = new Set<string>(LANGUAGES.map((l) => l.id));

export function isLanguageId(value: unknown): value is LanguageId {
	return typeof value === 'string' && IDS.has(value);
}

/** Wraps a CodeMirror 5-style mode from @codemirror/legacy-modes. */
async function legacy(parser: Promise<StreamParser<unknown>>): Promise<Extension> {
	const [{ StreamLanguage }, mode] = await Promise.all([import('@codemirror/language'), parser]);
	return StreamLanguage.define(mode);
}

const LOADERS: Record<Exclude<LanguageId, 'plaintext'>, () => Promise<Extension>> = {
	c: () => import('@codemirror/lang-cpp').then((m) => m.cpp()),
	cpp: () => import('@codemirror/lang-cpp').then((m) => m.cpp()),
	csharp: () => legacy(import('@codemirror/legacy-modes/mode/clike').then((m) => m.csharp)),
	css: () => import('@codemirror/lang-css').then((m) => m.css()),
	go: () => import('@codemirror/lang-go').then((m) => m.go()),
	html: () => import('@codemirror/lang-html').then((m) => m.html()),
	java: () => import('@codemirror/lang-java').then((m) => m.java()),
	javascript: () => import('@codemirror/lang-javascript').then((m) => m.javascript({ jsx: true })),
	json: () => import('@codemirror/lang-json').then((m) => m.json()),
	kotlin: () => legacy(import('@codemirror/legacy-modes/mode/clike').then((m) => m.kotlin)),
	markdown: () => import('@codemirror/lang-markdown').then((m) => m.markdown()),
	php: () => import('@codemirror/lang-php').then((m) => m.php()),
	python: () => import('@codemirror/lang-python').then((m) => m.python()),
	ruby: () => legacy(import('@codemirror/legacy-modes/mode/ruby').then((m) => m.ruby)),
	rust: () => import('@codemirror/lang-rust').then((m) => m.rust()),
	shell: () => legacy(import('@codemirror/legacy-modes/mode/shell').then((m) => m.shell)),
	sql: () => import('@codemirror/lang-sql').then((m) => m.sql()),
	swift: () => legacy(import('@codemirror/legacy-modes/mode/swift').then((m) => m.swift)),
	typescript: () =>
		import('@codemirror/lang-javascript').then((m) =>
			m.javascript({ typescript: true, jsx: true })
		),
	yaml: () => import('@codemirror/lang-yaml').then((m) => m.yaml())
};

const cache = new Map<LanguageId, Promise<Extension>>();

/**
 * Resolves the editor extensions for a language: the grammar plus syntax
 * highlighting, bracket matching and electric indentation. Plain text needs
 * none of it and resolves to an empty extension without loading anything.
 * Results are cached; a failed load (e.g. offline) is evicted so it can be
 * retried.
 */
export function loadLanguage(id: LanguageId): Promise<Extension> {
	if (id === 'plaintext') return Promise.resolve([]);
	let pending = cache.get(id);
	if (!pending) {
		pending = Promise.all([
			import('@codemirror/language'),
			import('./highlight'),
			LOADERS[id]()
		]).then(([{ bracketMatching, indentOnInput }, { highlighting }, lang]) => [
			lang,
			highlighting,
			bracketMatching(),
			indentOnInput()
		]);
		pending.catch(() => cache.delete(id));
		cache.set(id, pending);
	}
	return pending;
}
