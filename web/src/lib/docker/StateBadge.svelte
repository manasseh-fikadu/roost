<!-- A container's state in a word, in the color of how it's doing. -->
<script lang="ts">
	import type { DockerContainerRow } from '#lib/api/types.js';
	import Badge from '#lib/ui/Badge.svelte';
	import { stateBadge } from './docker.js';

	interface Props {
		container: Pick<DockerContainerRow, 'state' | 'health' | 'exitCode' | 'oomKilled'>;
		class?: string;
	}

	let { container, class: className }: Props = $props();

	const badge = $derived(stateBadge(container));
</script>

<Badge tone={badge.tone} mono class={className}>{badge.label.toUpperCase()}</Badge>
