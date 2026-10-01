// Fails if the JavaScript needed to open a pad exceeds the budget.
//
// "Initial" = every Vite entry chunk (SvelteKit's start/app entries and each
// route node) plus everything they import statically, measured as the sum of
// per-file gzip -9 sizes, which is how browsers download them. Lazily
// imported chunks (language grammars, highlighting) are excluded. Run after
// `pnpm build`.
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { gzipSync } from 'node:zlib';

const BUDGET_KB = Number(process.env.BUNDLE_BUDGET_KB ?? 200);
const root = join(import.meta.dirname, '..', '.svelte-kit', 'output', 'client');
const manifest = JSON.parse(readFileSync(join(root, '.vite', 'manifest.json'), 'utf8'));

const files = new Set();
const visit = (key) => {
	const chunk = manifest[key];
	if (!chunk || files.has(chunk.file)) return;
	files.add(chunk.file);
	for (const dep of chunk.imports ?? []) visit(dep);
};
for (const [key, chunk] of Object.entries(manifest)) if (chunk.isEntry) visit(key);

const sizes = [...files]
	.filter((f) => f.endsWith('.js'))
	.map((f) => ({ file: f, gz: gzipSync(readFileSync(join(root, f)), { level: 9 }).length }))
	.sort((a, b) => b.gz - a.gz);
const totalKB = sizes.reduce((n, s) => n + s.gz, 0) / 1024;

for (const s of sizes.slice(0, 5))
	console.log(`${(s.gz / 1024).toFixed(1).padStart(7)} KB  ${s.file}`);
console.log(
	`initial JS: ${totalKB.toFixed(1)} KB gzipped in ${sizes.length} files (budget ${BUDGET_KB} KB)`
);
if (totalKB > BUDGET_KB) {
	console.error(
		`Over budget by ${(totalKB - BUDGET_KB).toFixed(1)} KB. See CLAUDE.md "Bundle budget".`
	);
	process.exit(1);
}
