<script lang="ts">
	import * as NativeSelect from '$lib/components/ui/native-select';
	import { LANGUAGES, isLanguageId, type LanguageId } from '$lib/editor/languages';

	let {
		value,
		onchange,
		disabled = false
	}: { value: LanguageId; onchange: (id: LanguageId) => void; disabled?: boolean } = $props();
</script>

<NativeSelect.Root
	size="sm"
	aria-label="Language"
	{value}
	{disabled}
	onchange={(e) => {
		const id = e.currentTarget.value;
		if (isLanguageId(id)) onchange(id);
	}}
>
	{#each LANGUAGES as lang (lang.id)}
		<NativeSelect.Option value={lang.id}>{lang.label}</NativeSelect.Option>
	{/each}
</NativeSelect.Root>
