import tailwindcss from '@tailwindcss/vite';
import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vitest/config';

const apiTarget = process.env.PAIRPAD_API ?? 'http://localhost:8080';

export default defineConfig({
	plugins: [
		tailwindcss(),
		sveltekit({
			compilerOptions: {
				// Force runes mode for the project, except for libraries. Can be removed in svelte 6.
				runes: ({ filename }) =>
					filename.split(/[/\\]/).includes('node_modules') ? undefined : true
			},
			// precompress writes .br and .gz next to every asset so the
			// server can send them as-is instead of compressing per request.
			adapter: adapter({ fallback: 'index.html', precompress: true })
		})
	],
	resolve: {
		alias: [
			// shadcn's Button builds classes with tv() and then merges them with
			// cn(), which already resolves Tailwind conflicts. The lite build
			// skips tv's own tailwind-merge pass, saving ~13 KB gzipped from
			// the initial bundle with identical output. An alias keeps the
			// generated ui/ files untouched.
			{ find: /^tailwind-variants$/, replacement: 'tailwind-variants/lite' }
		]
	},
	test: {
		include: ['src/**/*.test.ts'],
		environment: 'node'
	},
	server: {
		proxy: {
			'/api': apiTarget,
			'/ws': { target: apiTarget, ws: true }
		}
	}
});
