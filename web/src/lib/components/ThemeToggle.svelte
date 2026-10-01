<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import { theme, type ThemeMode } from '$lib/theme.svelte';
	import MonitorIcon from '@lucide/svelte/icons/monitor';
	import MoonIcon from '@lucide/svelte/icons/moon';
	import SunIcon from '@lucide/svelte/icons/sun';

	const LABELS: Record<ThemeMode, string> = { system: 'System', light: 'Light', dark: 'Dark' };

	const label = $derived(`Theme: ${LABELS[theme.mode]}. Switch to ${LABELS[theme.next()]}`);
</script>

<Button
	variant="ghost"
	size="icon-sm"
	aria-label={label}
	title={label}
	onclick={() => theme.set(theme.next())}
>
	{#if theme.mode === 'light'}
		<SunIcon />
	{:else if theme.mode === 'dark'}
		<MoonIcon />
	{:else}
		<MonitorIcon />
	{/if}
</Button>
