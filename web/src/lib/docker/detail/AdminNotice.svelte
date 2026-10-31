<!--
	What a tab shows a viewer, for what only admins get. The server withholds it
	either way; this says so instead of leaving the tab empty.
-->
<script lang="ts">
	import Lock from '@lucide/svelte/icons/lock';
	import Card from '#lib/ui/Card.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';

	interface Props {
		/** What admins can do here, as in "read container logs". */
		what: string;
	}

	let { what }: Props = $props();
</script>

<Card>
	<EmptyState title="Only admins can {what}" icon={Lock}>
		It can hold passwords and other secrets, so Roost shows it to admins only.
	</EmptyState>
</Card>
