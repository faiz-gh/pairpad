<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { Button } from '$lib/components/ui/button';
	import { createRoom } from '$lib/api';
	import type { SyncStatus } from '$lib/sync/provider.svelte';
	import { cn } from '$lib/utils';
	import CheckIcon from '@lucide/svelte/icons/check';
	import LinkIcon from '@lucide/svelte/icons/link';
	import PlusIcon from '@lucide/svelte/icons/plus';

	let { status }: { status: SyncStatus } = $props();

	let creating = $state(false);
	let copied = $state(false);
	let copiedTimer: ReturnType<typeof setTimeout> | undefined;

	const statusLabel: Record<SyncStatus, string> = {
		connecting: 'Connecting…',
		synced: 'Live',
		offline: 'Offline — reconnecting'
	};

	async function newPad() {
		creating = true;
		try {
			const room = await createRoom();
			await goto(resolve('/[roomId]', { roomId: room.id }));
		} finally {
			creating = false;
		}
	}

	async function copyLink() {
		await navigator.clipboard.writeText(location.href);
		copied = true;
		clearTimeout(copiedTimer);
		copiedTimer = setTimeout(() => (copied = false), 1500);
	}
</script>

<header class="flex items-center gap-2 border-b px-3 py-2">
	<a href={resolve('/')} class="mr-2 font-semibold tracking-tight">Pairpad</a>
	<Button size="sm" variant="outline" onclick={newPad} disabled={creating}>
		<PlusIcon data-icon="inline-start" />
		New pad
	</Button>
	<Button size="sm" variant="outline" onclick={copyLink}>
		{#if copied}
			<CheckIcon data-icon="inline-start" />
			Copied
		{:else}
			<LinkIcon data-icon="inline-start" />
			Copy link
		{/if}
	</Button>
	<span class="ml-auto flex items-center gap-2 text-sm text-muted-foreground" aria-live="polite">
		<span
			class={cn(
				'size-2 rounded-full',
				status === 'synced' ? 'bg-primary' : 'animate-pulse bg-muted-foreground'
			)}
		></span>
		{statusLabel[status]}
	</span>
</header>
