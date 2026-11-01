<!--
	What a demo shows in place of a page that needs a real machine, such as the
	terminal: the page is off, and why.
-->
<script lang="ts">
	import FlaskConical from '@lucide/svelte/icons/flask-conical';
	import { resolve } from '$app/paths';
	import { buttonClass } from '#lib/ui/button.js';
	import EmptyState from '#lib/ui/EmptyState.svelte';

	interface Props {
		/** What's off, such as "Terminals". */
		what: string;
	}

	let { what }: Props = $props();
</script>

<EmptyState title="{what} are turned off in this demo" icon={FlaskConical}>
	The machines here are made up, so there's nothing to open. On your own tailnet, Bield opens a real
	shell and real files on every machine you set up SSH access for.
	<div class="mt-4">
		<a href={resolve('/devices')} class={buttonClass('secondary', 'sm')}>Back to the devices</a>
	</div>
</EmptyState>
