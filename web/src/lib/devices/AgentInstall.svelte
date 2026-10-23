<!--
	The command that installs the agent, which only admins can load, since it
	holds the token every agent sends. With `detailed`, also where the agent
	logs and the command that removes it.
-->
<script lang="ts">
	import { getAgentSetup } from '#lib/api/client.js';
	import CodeLine from '#lib/ui/CodeLine.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';

	interface Props {
		detailed?: boolean;
	}

	let { detailed = false }: Props = $props();

	const setup = getAgentSetup();
</script>

{#await setup}
	<Skeleton class="h-10" />
{:then s}
	<CodeLine command={s.installCommand} />
	{#if detailed}
		<p class="mt-2 text-[12px] text-fg-subtle">
			On macOS it runs as a LaunchAgent, which logs to
			<code class="font-mono">~/Library/Logs/roost-agent.log</code>. On Linux it's a systemd user
			service: <code class="font-mono">journalctl --user -u roost-agent</code>. Run the command
			again to update it to version {s.version}.
		</p>
		<div class="mt-5 mb-2 text-[13px] font-medium text-fg-muted">Uninstall</div>
		<CodeLine command={s.uninstallCommand} />
	{/if}
{:catch err}
	<p class="text-[13px] text-fg-subtle">
		Couldn't load the install command: {err instanceof Error ? err.message : String(err)}
	</p>
{/await}
