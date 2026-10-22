<!-- A page that isn't built yet: its header, and which issue builds it. -->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import Card from '#lib/ui/Card.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import type { Icon } from '#lib/icon.js';

	interface Props {
		title: string;
		description: string;
		icon: Icon;
		/** The GitHub issue that builds the page. */
		issue: number;
		/** What the page will show. */
		children: Snippet;
	}

	let { title, description, icon, issue, children }: Props = $props();
</script>

<PageHeader {title} {description} />
<Card>
	<EmptyState title="Coming soon" {icon}>
		{@render children()}
		<a
			href="https://github.com/manasseh-fikadu/roost/issues/{issue}"
			target="_blank"
			rel="noreferrer"
			class="mt-2 block font-mono text-[12px] text-fg-muted underline decoration-line-heavy underline-offset-4 transition-colors hover:text-fg"
		>
			Issue #{issue}
		</a>
	</EmptyState>
</Card>
