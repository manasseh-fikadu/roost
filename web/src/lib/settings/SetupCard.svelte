<!-- Opens the first-run setup again, for an admin who skipped it or wants to check Bield once more. -->
<script lang="ts">
	import { resolve } from '$app/paths';
	import Card from '#lib/ui/Card.svelte';
	import { buttonClass } from '#lib/ui/button.js';
</script>

<Card
	title="First-run setup"
	description="Check that Bield sees Tailscale, Docker and systemd, name it, confirm the admins, and connect more machines."
>
	<div class="p-4">
		<a href={resolve('/setup')} class={buttonClass('secondary', 'md')}>Open the setup</a>
	</div>
</Card>
