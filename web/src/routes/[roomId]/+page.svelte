<script lang="ts">
	import * as Y from 'yjs';
	import { page } from '$app/state';
	import { resolve } from '$app/paths';
	import * as Alert from '$lib/components/ui/alert';
	import { Button } from '$lib/components/ui/button';
	import Editor from '$lib/components/Editor.svelte';
	import LanguagePicker from '$lib/components/LanguagePicker.svelte';
	import Toolbar from '$lib/components/Toolbar.svelte';
	import { getRoom, roomSocketUrl } from '$lib/api';
	import { loadIdentity } from '$lib/sync/identity';
	import { PairpadProvider } from '$lib/sync/provider.svelte';
	import { RoomLanguage } from '$lib/sync/room-language.svelte';

	type Session = {
		doc: Y.Doc;
		text: Y.Text;
		language: RoomLanguage;
		provider: PairpadProvider;
	};

	const roomId = $derived(page.params.roomId ?? '');

	// Raw: Y.Doc and the provider must not be wrapped in reactive proxies.
	let session = $state.raw<Session | null>(null);
	let error = $state<'not-found' | 'failed' | null>(null);
	// The room was deleted (expired) while this tab had it open or offline.
	const expired = $derived(session?.provider.status === 'missing');

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
					language: new RoomLanguage(doc),
					provider: new PairpadProvider(
						doc,
						roomSocketUrl(room.id),
						loadIdentity(),
						async () => (await getRoom(room.id)) !== null
					)
				};
				session = current;
			})
			.catch(() => {
				if (!cancelled) error = 'failed';
			});

		return () => {
			cancelled = true;
			current?.provider.destroy();
			current?.language.destroy();
			current?.doc.destroy();
		};
	});
</script>

<svelte:head><title>{roomId} · Pairpad</title></svelte:head>

<div class="flex h-dvh flex-col">
	<Toolbar status={session?.provider.status ?? 'connecting'} peers={session?.provider.peers ?? []}>
		{#if session}
			<!-- Changes made before the first sync would never reach the server. -->
			<LanguagePicker
				value={session.language.current}
				disabled={!session.provider.hasSynced || expired}
				onchange={(id) => session?.language.set(id)}
			/>
		{/if}
	</Toolbar>
	<main class="min-h-0 flex-1">
		{#if error}
			<div class="flex h-full flex-col items-center justify-center gap-4 text-muted-foreground">
				<p>
					{error === 'not-found' ? `Pad "${roomId}" doesn't exist.` : "Couldn't load this pad."}
				</p>
				<Button href={resolve('/')}>Start a new pad</Button>
			</div>
		{:else if session}
			<div class="flex h-full flex-col">
				{#if session.provider.status === 'full'}
					<Alert.Root class="m-3 w-auto">
						<Alert.Title>This pad is full</Alert.Title>
						<Alert.Description>
							It already has the maximum number of people. You'll join automatically as soon as
							someone leaves.
						</Alert.Description>
					</Alert.Root>
				{/if}
				{#if expired}
					<Alert.Root class="m-3 w-auto">
						<Alert.Title>This pad has expired</Alert.Title>
						<Alert.Description>
							It was deleted after a long period of inactivity. The text below is your local copy
							and is read-only. Copy anything you need, then start a new pad.
						</Alert.Description>
						<Alert.Action>
							<Button size="sm" href={resolve('/')}>New pad</Button>
						</Alert.Action>
					</Alert.Root>
				{/if}
				<div class="min-h-0 flex-1">
					{#key session}
						<Editor
							text={session.text}
							awareness={session.provider.awareness}
							language={session.language.current}
							readOnly={!session.provider.hasSynced || expired}
						/>
					{/key}
				</div>
			</div>
		{/if}
	</main>
</div>
