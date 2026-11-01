<!--
	Who can use Bield: the people with a role, who else can view it, and how
	Bield joins the tailnet, which is only shown. Changes here apply at once,
	each as its own request, and every answer is the whole list of users, which
	replaces what's shown.
-->
<script lang="ts">
	import { onMount, tick } from 'svelte';
	import { page } from '$app/state';
	import Trash2 from '@lucide/svelte/icons/trash-2';
	import { RadioGroup } from 'bits-ui';
	import { getAccess, removeUser, setRole, setWhoCanView } from '#lib/api/client.js';
	import {
		RoleAdmin,
		RoleViewer,
		TailscaleModeEmbedded,
		WhoCanViewListed,
		WhoCanViewTailnet,
		type Access,
		type Role,
		type SettingsView,
		type User,
		type WhoCanView
	} from '#lib/api/types.js';
	import { roleLabel } from '#lib/access.js';
	import { shortDate } from '#lib/format.js';
	import { loginProblem } from '#lib/settings.js';
	import Badge from '#lib/ui/Badge.svelte';
	import Button from '#lib/ui/Button.svelte';
	import Card from '#lib/ui/Card.svelte';
	import DataRow from '#lib/ui/DataRow.svelte';
	import Dialog from '#lib/ui/Dialog.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import IconButton from '#lib/ui/IconButton.svelte';
	import Input from '#lib/ui/Input.svelte';
	import Select from '#lib/ui/Select.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import ErrorState from '#lib/ui/ErrorState.svelte';

	interface Props {
		/** For how Bield joins the tailnet. */
		view: SettingsView;
	}

	let { view }: Props = $props();

	const id = $props.id();

	const roles: Role[] = [RoleAdmin, RoleViewer];

	const whoOptions: { value: WhoCanView; title: string; description: string }[] = [
		{
			value: WhoCanViewListed,
			title: 'Only people listed above',
			description: 'Everyone else on the tailnet sees a page that tells them to ask an admin.'
		},
		{
			value: WhoCanViewTailnet,
			title: 'Anyone on the tailnet',
			description:
				'They can view as viewers, without logs, files or other secrets. Anyone with a role of their own keeps it.'
		}
	];

	let access = $state.raw<Access | null>(null);
	let loadError = $state<string | null>(null);
	let loading = $state(true);

	/** Counts the requests, and `applied` is the latest whose answer is shown. */
	let sequence = 0;
	let applied = 0;

	/** Shows an answer, unless a request that started after it has been answered already. */
	function take(mine: number, next: Access) {
		if (mine > applied) {
			applied = mine;
			access = next;
		}
	}

	async function load() {
		const mine = ++sequence;
		loading = true;
		try {
			take(mine, await getAccess());
			loadError = null;
		} catch (err) {
			loadError = err instanceof Error ? err.message : String(err);
		} finally {
			loading = false;
		}
	}

	onMount(() => {
		void load();
	});

	const myLogin = $derived(page.data.me?.login?.toLowerCase());

	function isMe(login: string): boolean {
		return myLogin !== undefined && login.toLowerCase() === myLogin;
	}

	// While a request for a login is out, its controls are off, and its role
	// shows the one asked for. If the request fails, the role goes back to the
	// hub's.
	let busy = $state<Record<string, boolean>>({});
	let shown = $state<Partial<Record<string, Role>>>({});

	/** Runs a request that changes one login, and says whether it worked. */
	async function change(login: string, as: Role | undefined, call: () => Promise<Access>) {
		const mine = ++sequence;
		busy[login] = true;
		if (as) {
			shown[login] = as;
		}
		try {
			take(mine, await call());
			return true;
		} catch {
			// The client showed the error.
			return false;
		} finally {
			delete busy[login];
			delete shown[login];
		}
	}

	/** Gives a login a role, or changes it, and says whether it worked. The toast says which. */
	function grant(login: string, role: Role): Promise<boolean> {
		const known = access?.users.some((u) => u.login.toLowerCase() === login.toLowerCase());
		return change(login, role, () => setRole(login, role, known ? 'Saved' : 'Added'));
	}

	function pick(user: User, role: Role) {
		if (role === user.role) {
			return;
		}
		if (isMe(user.login) && role !== RoleAdmin) {
			// Show the pick under the dialog, which takes it back if it's dismissed.
			shown[user.login] = role;
			ask('demote', user.login);
			return;
		}
		void grant(user.login, role);
	}

	/**
	 * Takes someone's role away. For anyone but the caller there's no question:
	 * the toast says it's done and can put the role back. The caller is asked,
	 * since taking their own role away can't be undone by them.
	 */
	async function remove(user: User) {
		if (isMe(user.login)) {
			ask('remove', user.login);
			return;
		}
		const undo = {
			label: 'Undo',
			run: () =>
				void change(user.login, user.role, () => setRole(user.login, user.role, 'Put back'))
		};
		if (await change(user.login, undefined, () => removeUser(user.login, undo))) {
			await focusAddForm();
		}
	}

	/** Gives focus back to the add form, once what had it is gone, unless a touch screen's keyboard would open. */
	async function focusAddForm() {
		await tick();
		if (matchMedia('(pointer: fine)').matches) {
			document.getElementById(`${id}-login`)?.focus();
		}
	}

	// The question before taking a role from the caller, or taking one away.
	// What it's about stays after it closes, so its text doesn't change while
	// it fades out. `fromForm` is for a demotion that came from the add form,
	// not from the person's role select.
	let question = $state<{ kind: 'remove' | 'demote'; login: string; fromForm: boolean } | null>(
		null
	);
	let questionOpen = $state(false);
	let answering = $state(false);

	function ask(kind: 'remove' | 'demote', login: string, fromForm = false) {
		question = { kind, login, fromForm };
		questionOpen = true;
	}

	function setQuestionOpen(open: boolean) {
		questionOpen = open;
		if (!open && question && !answering) {
			delete shown[question.login];
		}
	}

	async function confirm() {
		if (!question) {
			return;
		}
		const { kind, login, fromForm } = question;
		answering = true;
		let ok = false;
		try {
			if (kind === 'remove') {
				ok = await change(login, undefined, () => removeUser(login));
			} else if (fromForm) {
				ok = await submit(login, RoleViewer);
			} else {
				ok = await grant(login, RoleViewer);
			}
		} finally {
			answering = false;
			questionOpen = false;
		}
		if (ok && kind === 'remove') {
			// The row, and the button the dialog would give focus back to, are gone.
			// Wait for the dialog to let go of focus, then put it on the add form.
			await focusAddForm();
		}
	}

	// Adding a person.
	let newLogin = $state('');
	let newRole = $state<Role>(RoleViewer);
	let adding = $state(false);

	const problem = $derived(loginProblem(newLogin));
	const typed = $derived(newLogin.trim() !== '');

	/** Adds a person from the form, and clears the form if it worked. */
	async function submit(login: string, role: Role): Promise<boolean> {
		adding = true;
		const ok = await grant(login, role);
		adding = false;
		if (ok) {
			newLogin = '';
			newRole = RoleViewer;
		}
		return ok;
	}

	function add(e: SubmitEvent) {
		e.preventDefault();
		if (!typed || problem || adding) {
			return;
		}
		const login = newLogin.trim();
		if (isMe(login) && newRole !== RoleAdmin) {
			ask('demote', login, true);
			return;
		}
		void submit(login, newRole);
	}

	// Who can view.
	let pendingWho = $state<WhoCanView | null>(null);

	async function pickWho(who: WhoCanView) {
		if (!access || pendingWho || who === access.whoCanView) {
			return;
		}
		const mine = ++sequence;
		pendingWho = who;
		try {
			take(mine, await setWhoCanView(who));
		} catch {
			// The client showed the error.
		} finally {
			pendingWho = null;
		}
	}

	const whoValue = $derived(pendingWho ?? access?.whoCanView ?? '');
	const removingMyself = $derived(question?.kind === 'remove' && isMe(question.login));
	const questionTitle = $derived(
		question?.kind === 'demote' ? 'Make yourself a viewer?' : `Remove ${question?.login ?? ''}?`
	);
	const questionLede = $derived(
		question?.kind === 'demote'
			? "You'd stop being an admin."
			: `${question?.login ?? 'They'} loses their role.`
	);
</script>

<div class="space-y-6">
	<Card
		title="People"
		description="Admins can change anything. Viewers can look at the dashboard, but not logs, files or secrets."
	>
		{#if !access}
			{#if loadError}
				<ErrorState
					title="Couldn't load the people"
					message={loadError}
					{loading}
					onretry={() => void load()}
				/>
			{:else}
				<div class="space-y-2 p-4" aria-busy="true">
					<Skeleton class="h-12" />
					<Skeleton class="h-12" />
					<Skeleton class="h-12" />
				</div>
			{/if}
		{:else}
			{#if access.users.length === 0}
				<EmptyState title="No one has a role yet">
					Add the first person below. They'll be an admin if you pick Admin.
				</EmptyState>
			{:else}
				<ul class="divide-y divide-line">
					{#each access.users as u (u.login)}
						<li class="flex items-center gap-3 px-4 py-2.5">
							<div class="min-w-0 flex-1">
								<div class="flex items-center gap-2">
									<span class="truncate text-[13.5px] text-fg">{u.login}</span>
									{#if isMe(u.login)}
										<Badge tone="blue">you</Badge>
									{/if}
								</div>
								<p class="text-[12px] text-fg-subtle">Since {shortDate(u.since)}</p>
							</div>
							<div class="w-28 shrink-0">
								<Select
									aria-label="Role of {u.login}"
									disabled={busy[u.login]}
									bind:value={() => shown[u.login] ?? u.role, (v) => pick(u, v as Role)}
								>
									{#each roles as r (r)}
										<option value={r}>{roleLabel(r)}</option>
									{/each}
								</Select>
							</div>
							<IconButton
								label="Remove {u.login}"
								disabled={busy[u.login]}
								onclick={() => void remove(u)}
							>
								<Trash2 class="size-4" />
							</IconButton>
						</li>
					{/each}
				</ul>
			{/if}
			<form
				class="flex flex-col gap-2 border-t border-line bg-canvas/40 px-4 py-3 sm:flex-row sm:items-start"
				onsubmit={add}
			>
				<div class="min-w-0 flex-1">
					<Input
						id="{id}-login"
						bind:value={newLogin}
						type="text"
						inputmode="email"
						placeholder="alice@example.com"
						aria-label="Tailscale login of the person to add"
						aria-describedby="{id}-login-hint"
						autocomplete="off"
						autocapitalize="off"
						spellcheck={false}
					/>
					<p
						id="{id}-login-hint"
						class={['mt-1.5 text-[12px]', typed && problem ? 'text-red-fg' : 'text-fg-subtle']}
					>
						{typed && problem
							? problem
							: 'Their Tailscale login, the one they sign in to Tailscale with.'}
					</p>
				</div>
				<div class="sm:w-28">
					<Select bind:value={newRole} aria-label="Role of the person to add">
						{#each roles as r (r)}
							<option value={r}>{roleLabel(r)}</option>
						{/each}
					</Select>
				</div>
				<Button
					type="submit"
					variant="primary"
					size="md"
					class="h-9"
					loading={adding}
					disabled={!typed || problem !== ''}
				>
					Add
				</Button>
			</form>
		{/if}
	</Card>

	{#if access || !loadError}
		<Card title="Who can view" description="Besides the people above.">
			{#if !access}
				<div class="space-y-2 p-4" aria-busy="true">
					<Skeleton class="h-16" />
					<Skeleton class="h-16" />
				</div>
			{:else}
				<div class="p-4">
					<RadioGroup.Root
						aria-label="Who can view Bield"
						disabled={pendingWho !== null}
						class="space-y-2"
						bind:value={() => whoValue, (v) => void pickWho(v as WhoCanView)}
					>
						{#each whoOptions as o (o.value)}
							<RadioGroup.Item
								value={o.value}
								class="group flex w-full items-start gap-3 rounded-lg border border-line px-3 py-2.5 text-left transition-colors hover:border-line-strong hover:bg-hover/50 disabled:opacity-60 data-[state=checked]:border-line-heavy data-[state=checked]:bg-hover/60"
							>
								<span
									class="mt-0.5 flex size-4 shrink-0 items-center justify-center rounded-full border border-line-heavy group-data-[state=checked]:border-fg"
									aria-hidden="true"
								>
									<span
										class="size-2 scale-0 rounded-full bg-fg transition-transform group-data-[state=checked]:scale-100"
									></span>
								</span>
								<span class="min-w-0">
									<span class="block text-[13.5px] font-medium text-fg">{o.title}</span>
									<span class="mt-0.5 block text-[12.5px] text-fg-subtle">{o.description}</span>
								</span>
							</RadioGroup.Item>
						{/each}
					</RadioGroup.Root>
				</div>
			{/if}
		</Card>
	{/if}

	<Card title="Tailscale" description="How Bield joins your tailnet.">
		<dl>
			<DataRow label="Mode"
				>{view.tailscale === TailscaleModeEmbedded ? 'Embedded' : 'Host'}</DataRow
			>
		</dl>
		<p class="px-4 py-3 text-[13px] text-fg-subtle">
			{view.tailscale === TailscaleModeEmbedded
				? "Bield runs its own Tailscale node, so it's a machine of its own on your tailnet."
				: 'Bield uses the Tailscale already running on its machine.'}
			It's set when Bield starts, with
			<code class="font-mono text-[12px] text-fg-muted">--tailscale</code> or
			<code class="font-mono text-[12px] text-fg-muted">BIELD_TAILSCALE</code>, and can't be changed
			here.
		</p>
	</Card>
</div>

<Dialog
	bind:open={() => questionOpen, setQuestionOpen}
	title={questionTitle}
	description={questionLede}
>
	<div class="space-y-2 text-[13px] text-fg-muted">
		{#if question?.kind === 'demote'}
			<p>
				You'll lose access to Settings at once, and only another admin can make you an admin again.
			</p>
		{:else}
			<p>
				{#if access?.whoCanView === WhoCanViewTailnet}
					They can still view Bield as a viewer, because anyone on the tailnet can.
				{:else}
					They won't be able to view Bield any more.
				{/if}
			</p>
			{#if removingMyself}
				<p>You'll lose access to Settings at once.</p>
			{/if}
		{/if}
	</div>
	{#snippet footer()}
		<Button variant="ghost" onclick={() => setQuestionOpen(false)}>Cancel</Button>
		<Button variant="danger" loading={answering} onclick={() => void confirm()}>
			{question?.kind === 'demote' ? 'Make me a viewer' : 'Remove'}
		</Button>
	{/snippet}
</Dialog>
