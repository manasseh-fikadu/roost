<!--
	A container's labels, by name. The ones that look like credentials come only
	to admins, and stay hidden until revealed.
-->
<script lang="ts">
	import type { DockerContainerDetail } from '#lib/api/types.js';
	import { labelRows } from './helpers.js';
	import KeyValueList from './KeyValueList.svelte';

	interface Props {
		container: DockerContainerDetail;
	}

	let { container: d }: Props = $props();

	const rows = $derived(labelRows(d.labels, d.secretLabels));
</script>

<KeyValueList title="Labels" noun="label" {rows} empty="No labels" />
