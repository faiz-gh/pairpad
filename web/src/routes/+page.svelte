<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { Button } from '$lib/components/ui/button';
	import { createRoom, createRoomErrorMessage } from '$lib/api';

	let failure = $state<string | null>(null);

	async function start() {
		failure = null;
		try {
			const room = await createRoom();
			await goto(resolve('/[roomId]', { roomId: room.id }), { replaceState: true });
		} catch (err) {
			failure = createRoomErrorMessage(err);
		}
	}

	onMount(start);
</script>

<svelte:head><title>Pairpad</title></svelte:head>

<main class="flex h-dvh flex-col items-center justify-center gap-4 text-muted-foreground">
	{#if failure}
		<p>{failure}</p>
		<Button onclick={start}>Try again</Button>
	{:else}
		<p>Creating a pad…</p>
	{/if}
</main>
