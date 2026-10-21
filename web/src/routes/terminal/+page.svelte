<script lang="ts">
	import SquareTerminal from '@lucide/svelte/icons/square-terminal';
	import Placeholder from '#lib/shell/Placeholder.svelte';
</script>

<Placeholder
	title="Terminal"
	description="A shell on any machine, in the browser."
	icon={SquareTerminal}
	issue={27}
>
	Terminal sessions over SSH that survive a closed tab, in tabs of their own.
</Placeholder>
