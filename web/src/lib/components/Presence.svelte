<script lang="ts">
	import * as Avatar from '$lib/components/ui/avatar';
	import type { Peer } from '$lib/sync/provider.svelte';
	import { cn } from '$lib/utils';

	const MAX_SHOWN = 5;

	let { peers, class: className }: { peers: Peer[]; class?: string } = $props();

	const shown = $derived(peers.slice(0, MAX_SHOWN));
	const hidden = $derived(peers.slice(MAX_SHOWN));
	const label = $derived(
		peers.length === 1
			? 'Only you are here'
			: `${peers.length} people here: ${peers.map((p) => p.name).join(', ')}`
	);

	function initials(name: string): string {
		return name
			.split(/\s+/)
			.map((w) => w[0] ?? '')
			.join('')
			.slice(0, 2)
			.toUpperCase();
	}
</script>

{#if peers.length > 0}
	<div class={cn('flex items-center', className)} role="group" aria-label={label}>
		<Avatar.Group>
			{#each shown as peer (peer.clientId)}
				<Avatar.Root size="sm" title={peer.self ? `${peer.name} (you)` : peer.name}>
					<!-- Each peer's color matches their cursor in the editor. -->
					<Avatar.Fallback
						class="font-medium text-neutral-900"
						style="background-color: {peer.color}"
					>
						{initials(peer.name)}
					</Avatar.Fallback>
				</Avatar.Root>
			{/each}
			{#if hidden.length > 0}
				<Avatar.GroupCount title={hidden.map((p) => p.name).join(', ')}>
					+{hidden.length}
				</Avatar.GroupCount>
			{/if}
		</Avatar.Group>
	</div>
{/if}
