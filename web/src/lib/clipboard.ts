/**
 * Copies text to the clipboard. The async Clipboard API only exists in
 * secure contexts (HTTPS or localhost); Pairpad is also served over plain
 * HTTP on a bare IP or tailnet hostname, so fall back to the legacy
 * execCommand path there. Returns false if neither works.
 */
export async function copyText(text: string): Promise<boolean> {
	if (window.isSecureContext && navigator.clipboard) {
		try {
			await navigator.clipboard.writeText(text);
			return true;
		} catch {
			// Permission denied or document not focused: try the fallback.
		}
	}
	const area = document.createElement('textarea');
	area.value = text;
	area.setAttribute('readonly', '');
	area.style.position = 'fixed';
	area.style.opacity = '0';
	document.body.append(area);
	const active = document.activeElement as HTMLElement | null;
	area.select();
	try {
		return document.execCommand('copy');
	} catch {
		return false;
	} finally {
		area.remove();
		active?.focus();
	}
}
