<!--
	How to update the agent, and the command that removes it, which only
	admins can load.
-->
<script lang="ts">
	import { getAgentSetup } from '#lib/api/client.js';
	import CodeLine from '#lib/ui/CodeLine.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';

	const setup = getAgentSetup();
</script>

{#await setup}
	<Skeleton class="h-10" />
{:then s}
	<p>
		To update an agent to version {s.version}, enroll it again: run a new install command on its
		machine.
	</p>
	<div class="mt-4 mb-2 font-medium text-fg-muted">Uninstall</div>
	<CodeLine command={s.uninstallCommand} />
	<p class="mt-2 text-[12px] text-fg-subtle">
		Run it on the machine. It also tells Bield to forget the agent.
	</p>
{:catch err}
	<p class="text-fg-subtle">
		Couldn't load the uninstall command: {err instanceof Error ? err.message : String(err)}
	</p>
{/await}
