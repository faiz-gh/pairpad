<script lang="ts">
	import './layout.css';
	import { theme } from '$lib/theme.svelte';

	let { children } = $props();

	$effect(() => theme.apply());
</script>

{@render children()}
