<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { Button } from '$lib/components/ui/button';
	import { createRoom, createRoomErrorMessage } from '$lib/api';
	import { notice } from '$lib/notice.svelte';
	import { copyText } from '$lib/clipboard';
	import Presence from '$lib/components/Presence.svelte';
	import ThemeToggle from '$lib/components/ThemeToggle.svelte';
	import type { Peer, SyncStatus } from '$lib/sync/provider.svelte';
	import { cn } from '$lib/utils';
	import CheckIcon from '@lucide/svelte/icons/check';
	import LinkIcon from '@lucide/svelte/icons/link';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import type { Snippet } from 'svelte';

	let { status, peers, children }: { status: SyncStatus; peers: Peer[]; children?: Snippet } =
		$props();

	let creating = $state(false);
	let copied = $state(false);
	let copiedTimer: ReturnType<typeof setTimeout> | undefined;

	const statusLabel: Record<SyncStatus, string> = {
		connecting: 'Connecting…',
		synced: 'Live',
		offline: 'Offline — reconnecting',
		missing: 'Pad expired',
		full: 'Pad full — waiting'
	};

	async function newPad() {
		creating = true;
		try {
			const room = await createRoom();
			await goto(resolve('/[roomId]', { roomId: room.id }));
		} catch (err) {
			notice.show(createRoomErrorMessage(err));
		} finally {
			creating = false;
		}
	}

	async function copyLink() {
		if (!(await copyText(location.href))) {
			notice.show(`Couldn't copy automatically. The link is ${location.href}`);
			return;
		}
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
	{@render children?.()}
	{#if notice.text}
		<p class="ml-2 truncate text-sm text-muted-foreground" role="status">{notice.text}</p>
	{/if}
	<Presence {peers} class="ml-auto" />
	<span class="flex items-center gap-2 text-sm text-muted-foreground" aria-live="polite">
		<span
			class={cn(
				'size-2 rounded-full',
				status === 'synced' && 'bg-primary',
				(status === 'missing' || status === 'full') && 'bg-muted-foreground',
				(status === 'connecting' || status === 'offline') && 'animate-pulse bg-muted-foreground'
			)}
		></span>
		{statusLabel[status]}
	</span>
	<ThemeToggle />
</header>
