<script lang="ts">
	import * as Y from 'yjs';
	import { page } from '$app/state';
	import { resolve } from '$app/paths';
	import { Button } from '$lib/components/ui/button';
	import Editor from '$lib/components/Editor.svelte';
	import Toolbar from '$lib/components/Toolbar.svelte';
	import { getRoom, roomSocketUrl } from '$lib/api';
	import { PairpadProvider } from '$lib/sync/provider.svelte';

	type Session = { doc: Y.Doc; text: Y.Text; provider: PairpadProvider };

	const roomId = $derived(page.params.roomId ?? '');

	// Raw: Y.Doc and the provider must not be wrapped in reactive proxies.
	let session = $state.raw<Session | null>(null);
	let error = $state<'not-found' | 'failed' | null>(null);

	// One doc + socket per room; "New pad" navigates between rooms without
	// remounting this page, so everything is torn down when roomId changes.
	$effect(() => {
		const id = roomId;
		let cancelled = false;
		let current: Session | null = null;
		session = null;
		error = null;

		getRoom(id)
			.then((room) => {
				if (cancelled) return;
				if (!room) {
					error = 'not-found';
					return;
				}
				const doc = new Y.Doc();
				current = {
					doc,
					text: doc.getText('content'),
					provider: new PairpadProvider(doc, roomSocketUrl(room.id))
				};
				session = current;
			})
			.catch(() => {
				if (!cancelled) error = 'failed';
			});

		return () => {
			cancelled = true;
			current?.provider.destroy();
			current?.doc.destroy();
		};
	});
</script>

<svelte:head><title>{roomId} · Pairpad</title></svelte:head>

<div class="flex h-dvh flex-col">
	<Toolbar status={session?.provider.status ?? 'connecting'} />
	<main class="min-h-0 flex-1">
		{#if error}
			<div class="flex h-full flex-col items-center justify-center gap-4 text-muted-foreground">
				<p>
					{error === 'not-found' ? `Pad "${roomId}" doesn't exist.` : "Couldn't load this pad."}
				</p>
				<Button href={resolve('/')}>Start a new pad</Button>
			</div>
		{:else if session}
			{#key session}
				<Editor text={session.text} readOnly={!session.provider.hasSynced} />
			{/key}
		{/if}
	</main>
</div>
