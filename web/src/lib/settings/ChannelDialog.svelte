<!--
	Where admins add a notification channel, or change one. The fields depend on
	the kind. A saved channel's secrets are never sent back, so their fields
	start empty, and leaving one empty keeps it.
-->
<script lang="ts">
	import { untrack } from 'svelte';
	import { addChannel, putChannel } from '#lib/api/client.js';
	import type { NotificationChannel } from '#lib/api/types.js';
	import {
		channelProblem,
		draftOf,
		inputOf,
		kinds,
		newDraft,
		securityOptions,
		severityOptions
	} from '#lib/notifications.js';
	import Button from '#lib/ui/Button.svelte';
	import Dialog from '#lib/ui/Dialog.svelte';
	import Field from '#lib/ui/Field.svelte';
	import Input from '#lib/ui/Input.svelte';
	import Select from '#lib/ui/Select.svelte';
	import Switch from '#lib/ui/Switch.svelte';
	import Textarea from '#lib/ui/Textarea.svelte';
	import ChannelIcon from './ChannelIcon.svelte';
	import SecretInput from './SecretInput.svelte';

	interface Props {
		/** The channel to change, or null to add one. */
		channel: NotificationChannel | null;
		open?: boolean;
		/** Called with the channel the hub saved. */
		onsaved: (channel: NotificationChannel) => void;
	}

	let { channel, open = $bindable(false), onsaved }: Props = $props();

	const id = $props.id();

	let draft = $state(newDraft('ntfy'));
	let saving = $state(false);
	/** Whether anything was typed, so the form only complains once it's started. */
	let touched = $state(false);

	// The form starts from the channel each time the dialog opens, and stays
	// put while it's open, even as the list refreshes.
	$effect.pre(() => {
		if (open) {
			untrack(() => {
				draft = channel ? draftOf(channel) : newDraft('ntfy');
				touched = false;
			});
		}
	});

	const problem = $derived(channelProblem(draft, channel));
	const has = (secret: 'url' | 'token' | 'password') => channel?.secrets.includes(secret) ?? false;

	async function save(e: SubmitEvent) {
		e.preventDefault();
		if (problem) {
			touched = true;
			return;
		}
		saving = true;
		try {
			const input = inputOf($state.snapshot(draft));
			const saved = channel ? await putChannel(channel.id, input) : await addChannel(input);
			onsaved(saved);
			open = false;
		} catch {
			// The client showed the error.
		} finally {
			saving = false;
		}
	}
</script>

<Dialog
	bind:open
	title={channel ? `Edit ${channel.name}` : 'Add a channel'}
	description="Bield sends a message here when an alert opens. You choose which alerts."
	width="sm:max-w-xl"
>
	<form
		id="{id}-form"
		class="grid gap-4 sm:grid-cols-2"
		oninput={() => (touched = true)}
		onsubmit={save}
		novalidate
	>
		{#if !channel}
			<fieldset class="grid grid-cols-3 gap-2 sm:col-span-2">
				<legend class="sr-only">Kind of channel</legend>
				{#each kinds as k (k.value)}
					<label class="cursor-pointer">
						<input
							type="radio"
							name="{id}-kind"
							value={k.value}
							bind:group={draft.kind}
							class="peer sr-only"
						/>
						<span
							class="flex flex-col items-center gap-1.5 rounded-lg border border-line-strong bg-canvas px-2 py-3 text-[12.5px] text-fg-muted transition-colors peer-checked:border-fg-subtle peer-checked:bg-hover peer-checked:text-fg peer-focus-visible:border-fg-subtle hover:border-line-heavy"
						>
							<ChannelIcon kind={k.value} class="size-5" />
							{k.label}
						</span>
					</label>
				{/each}
				<p class="col-span-3 text-[12px] text-fg-subtle">
					{kinds.find((k) => k.value === draft.kind)?.description}
				</p>
			</fieldset>
		{/if}

		<div class="sm:col-span-2">
			<Field label="Name" hint="Shown in the list and in the activity log.">
				<Input bind:value={draft.name} placeholder="My phone" maxlength={40} autocomplete="off" />
			</Field>
		</div>

		{#if draft.kind === 'ntfy'}
			<Field
				label="Topic"
				hint="On ntfy.sh anyone who knows a topic can read it, so pick one that's hard to guess."
			>
				<Input
					bind:value={draft.topic}
					placeholder="bield-alerts-k3x9q"
					autocomplete="off"
					autocapitalize="off"
					spellcheck={false}
				/>
			</Field>
			<Field label="Server" hint="Leave it empty for ntfy.sh.">
				<Input
					bind:value={draft.server}
					placeholder="https://ntfy.sh"
					inputmode="url"
					autocomplete="off"
					autocapitalize="off"
					spellcheck={false}
				/>
			</Field>
			<div class="sm:col-span-2">
				<SecretInput
					label="Access token"
					optional
					bind:value={draft.token}
					bind:cleared={draft.cleared.token}
					saved={has('token')}
					placeholder="tk_…"
					hint="For a topic that needs one. Or sign in with a username below."
				/>
			</div>
			<Field label="Username (optional)">
				<Input bind:value={draft.username} autocomplete="off" autocapitalize="off" />
			</Field>
			<SecretInput
				label="Password"
				optional
				bind:value={draft.password}
				bind:cleared={draft.cleared.password}
				saved={has('password')}
			/>
		{:else if draft.kind === 'discord'}
			<div class="sm:col-span-2">
				<SecretInput
					label="Webhook URL"
					bind:value={draft.url}
					bind:cleared={draft.cleared.url}
					saved={has('url')}
					placeholder="https://discord.com/api/webhooks/…"
					hint="In Discord: Channel settings, Integrations, Webhooks, New Webhook, Copy Webhook URL."
				/>
			</div>
		{:else if draft.kind === 'slack'}
			<div class="sm:col-span-2">
				<SecretInput
					label="Webhook URL"
					bind:value={draft.url}
					bind:cleared={draft.cleared.url}
					saved={has('url')}
					placeholder="https://hooks.slack.com/services/…"
					hint="From an Incoming Webhooks app in your Slack workspace."
				/>
			</div>
		{:else if draft.kind === 'telegram'}
			<div class="sm:col-span-2">
				<SecretInput
					label="Bot token"
					bind:value={draft.token}
					bind:cleared={draft.cleared.token}
					saved={has('token')}
					placeholder="123456789:AA…"
					hint="Message @BotFather to make a bot. Then send your bot a message, so it can reply."
				/>
			</div>
			<div class="sm:col-span-2">
				<Field
					label="Chat ID"
					hint="A number, such as 123456789, or @name for a public channel. @userinfobot tells you yours."
				>
					<Input
						bind:value={draft.chatId}
						placeholder="123456789"
						autocomplete="off"
						autocapitalize="off"
						spellcheck={false}
					/>
				</Field>
			</div>
		{:else if draft.kind === 'email'}
			<Field label="Mail server">
				<Input
					bind:value={draft.host}
					placeholder="smtp.example.com"
					autocomplete="off"
					autocapitalize="off"
					spellcheck={false}
				/>
			</Field>
			<Field label="Connection">
				<Select bind:value={draft.security}>
					{#each securityOptions as o (o.value)}
						<option value={o.value}>{o.label}</option>
					{/each}
				</Select>
			</Field>
			<Field label="Port" hint="Leave it empty for the connection's usual port.">
				<Input
					type="number"
					min={1}
					max={65535}
					placeholder={draft.security === 'tls' ? '465' : draft.security === 'none' ? '25' : '587'}
					value={Number.isNaN(draft.port) ? '' : draft.port}
					oninput={(e) => (draft.port = e.currentTarget.valueAsNumber)}
				/>
			</Field>
			<span class="hidden sm:block"></span>
			<Field label="Username (optional)">
				<Input bind:value={draft.username} autocomplete="off" autocapitalize="off" />
			</Field>
			<SecretInput
				label="Password"
				optional
				bind:value={draft.password}
				bind:cleared={draft.cleared.password}
				saved={has('password')}
			/>
			<div class="sm:col-span-2">
				<Field label="From">
					<Input
						bind:value={draft.from}
						placeholder="Bield <bield@example.com>"
						autocomplete="off"
						autocapitalize="off"
						spellcheck={false}
					/>
				</Field>
			</div>
			<div class="sm:col-span-2">
				<Field label="Send to" hint="One address per line, up to 10.">
					<Textarea
						bind:value={draft.to}
						rows={3}
						placeholder="you@example.com"
						autocomplete="off"
						autocapitalize="off"
						spellcheck={false}
					/>
				</Field>
			</div>
		{:else}
			<div class="sm:col-span-2">
				<SecretInput
					label="Address"
					bind:value={draft.url}
					bind:cleared={draft.cleared.url}
					saved={has('url')}
					placeholder="https://example.com/hook"
					hint="Bield posts JSON to it: the event, the alert and a text. The address can hold a token, so Bield keeps it secret."
				/>
			</div>
			<div class="sm:col-span-2">
				<SecretInput
					label="Bearer token"
					optional
					bind:value={draft.token}
					bind:cleared={draft.cleared.token}
					saved={has('token')}
					hint="Sent as an Authorization: Bearer header."
				/>
			</div>
		{/if}

		<div class="border-t border-line pt-4 sm:col-span-2">
			<p class="mb-3 text-[13px] font-medium text-fg">What it hears about</p>
			<div class="grid gap-4 sm:grid-cols-2">
				<Field label="Alerts">
					<Select bind:value={draft.minSeverity}>
						{#each severityOptions as o (o.value)}
							<option value={o.value}>{o.label}</option>
						{/each}
					</Select>
				</Field>
				<div class="space-y-2.5 sm:mt-7">
					<div class="flex items-center justify-between gap-3">
						<label for="{id}-resolve" class="text-[13px] text-fg-muted">
							Also when an alert resolves
						</label>
						<Switch id="{id}-resolve" bind:checked={draft.onResolve} />
					</div>
					{#if channel}
						<div class="flex items-center justify-between gap-3">
							<label for="{id}-enabled" class="text-[13px] text-fg-muted">Sending</label>
							<Switch id="{id}-enabled" bind:checked={draft.enabled} />
						</div>
					{/if}
				</div>
			</div>
		</div>
	</form>
	{#snippet footer()}
		<p class="mr-auto self-center text-[12.5px] text-red-fg" aria-live="polite">
			{touched ? problem : ''}
		</p>
		<Button variant="ghost" onclick={() => (open = false)}>Cancel</Button>
		<Button variant="primary" type="submit" form="{id}-form" loading={saving} disabled={!!problem}>
			{channel ? 'Save' : 'Add channel'}
		</Button>
	{/snippet}
</Dialog>
