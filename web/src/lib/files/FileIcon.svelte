<!-- The icon for a file, folder or link, colored by what it is. -->
<script lang="ts">
	import type { FileEntry } from '#lib/api/types.js';
	import { fileIcon } from './fileicon.js';

	interface Props {
		entry: Pick<FileEntry, 'name' | 'type' | 'targetType'>;
		class?: string;
	}

	let { entry, class: className }: Props = $props();

	const found = $derived(fileIcon(entry));
	const Icon = $derived(found.icon);
</script>

<Icon class={['size-4 shrink-0', found.tone, className]} />
