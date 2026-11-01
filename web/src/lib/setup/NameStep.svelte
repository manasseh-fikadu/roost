<!--
	The second step of the setup: what Bield is called, and who can change
	things. The name is saved by the page when the admin goes on.
-->
<script lang="ts">
	import { resolve } from '$app/paths';
	import { getAccess } from '#lib/api/client.js';
	import { RoleAdmin, WhoCanViewTailnet } from '#lib/api/types.js';
	import { shortDate } from '#lib/format.js';
	import Badge from '#lib/ui/Badge.svelte';
	import Card from '#lib/ui/Card.svelte';
	import Field from '#lib/ui/Field.svelte';
	import Input from '#lib/ui/Input.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';

	interface Props {
		/** What the admin typed as the name. */
		name: string;
		/** Why the name can't be saved, or ''. */
		problem: string;
		/** Called when the admin presses Enter in the name field. */
		onsubmit: () => void;
	}

	let { name = $bindable(), problem, onsubmit }: Props = $props();

	const access = getAccess();
</script>

<div class="space-y-4">
	<Card title="Name your Bield" description="Shown in the sidebar and in the title of every page.">
		<form
			class="max-w-md p-4"
			onsubmit={(e) => {
				e.preventDefault();
				onsubmit();
			}}
			novalidate
		>
			<Field label="Name">
				<Input
					bind:value={name}
					maxlength={40}
					autocomplete="off"
					aria-invalid={problem ? true : undefined}
				/>
			</Field>
			{#if problem}
				<p role="alert" class="mt-2 text-[12px] text-red-fg">{problem}</p>
			{/if}
		</form>
	</Card>

	<Card
		title="Confirm the admins"
		description="Admins can change settings, open terminals and files, and manage Docker. Everyone else you let in can only look."
	>
		{#await access}
			<div class="space-y-2 p-4"><Skeleton class="h-9" /><Skeleton class="h-9" /></div>
		{:then a}
			{@const admins = a.users.filter((u) => u.role === RoleAdmin)}
			<ul class="divide-y divide-line">
				{#each admins as u (u.login)}
					<li class="flex items-center gap-3 px-4 py-2.5 text-[13px]">
						<span class="min-w-0 flex-1 truncate font-mono text-fg">{u.login}</span>
						<Badge tone="blue">Admin</Badge>
						<span class="hidden text-fg-subtle sm:inline">since {shortDate(u.since)}</span>
					</li>
				{:else}
					<li class="px-4 py-3 text-[13px] text-fg-subtle">No one is an admin yet.</li>
				{/each}
			</ul>
			<p class="border-t border-line px-4 py-3 text-[12.5px] text-fg-subtle">
				{a.whoCanView === WhoCanViewTailnet
					? 'Anyone who signs in from the tailnet can view Bield.'
					: 'Only the people with a role can open Bield.'}
				Add people, or change who can view it, in
				<a
					href="{resolve('/settings')}?tab=access"
					class="text-fg-muted underline decoration-line-heavy underline-offset-4 hover:text-fg"
					>Settings</a
				>.
			</p>
		{:catch err}
			<p class="p-4 text-[13px] text-red-fg">
				Couldn't load the people: {err instanceof Error ? err.message : String(err)}
			</p>
		{/await}
	</Card>
</div>
