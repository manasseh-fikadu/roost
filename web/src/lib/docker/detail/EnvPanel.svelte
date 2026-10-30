<!-- A container's environment variables, by name, with every value hidden until revealed. -->
<script lang="ts">
	import type { DockerContainerDetail } from '#lib/api/types.js';
	import { envRows } from './helpers.js';
	import KeyValueList from './KeyValueList.svelte';

	interface Props {
		container: DockerContainerDetail;
	}

	let { container: d }: Props = $props();

	const rows = $derived(envRows(d.env));
</script>

<KeyValueList
	title="Environment variables"
	description="Values stay hidden until you reveal them. Copying one copies the real value."
	noun="variable"
	{rows}
	empty="No environment variables"
/>
