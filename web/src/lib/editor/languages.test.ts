import { describe, expect, it } from 'vitest';
import { DEFAULT_LANGUAGE, LANGUAGES, isLanguageId, loadLanguage } from './languages';

describe('languages', () => {
	it('has unique ids and includes the default', () => {
		const ids = LANGUAGES.map((l) => l.id);
		expect(new Set(ids).size).toBe(ids.length);
		expect(ids).toContain(DEFAULT_LANGUAGE);
	});

	it('validates ids', () => {
		expect(isLanguageId('python')).toBe(true);
		expect(isLanguageId('cobol')).toBe(false);
		expect(isLanguageId(42)).toBe(false);
		expect(isLanguageId(undefined)).toBe(false);
	});

	it('plain text loads nothing', async () => {
		expect(await loadLanguage('plaintext')).toEqual([]);
	});

	it.each(LANGUAGES.filter((l) => l.id !== 'plaintext').map((l) => l.id))(
		'%s loads grammar plus highlighting support',
		async (id) => {
			const ext = await loadLanguage(id);
			expect(Array.isArray(ext) && ext.length).toBe(4);
		}
	);

	it('caches loads', () => {
		expect(loadLanguage('go')).toBe(loadLanguage('go'));
	});
});
