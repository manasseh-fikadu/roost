<!--
	Under "No open alerts": if no notification channel is on, how to get the
	next alert without opening Roost. It says nothing once there's a channel
	on. Only admins can see channels, so wrap it in <AdminOnly>.
-->
<script lang="ts">
	import { resolve } from '$app/paths';
	import { getChannels } from '#lib/api/client.js';
	import { buttonClass } from '#lib/ui/button.js';

	/** Whether there's a channel that's on, or null until Roost says. */
	let on = $state<boolean | null>(null);

	$effect(() => {
		let current = true;
		getChannels()
			.then((list) => {
				if (current) {
					on = list.channels.some((c) => c.enabled);
				}
			})
			.catch(() => {
				// Without the list, there's nothing to suggest.
			});
		return () => {
			current = false;
		};
	});
</script>

{#if on === false}
	<div class="mt-3">
		<a href="{resolve('/settings')}?tab=notifications" class={buttonClass('secondary', 'sm')}>
			Send alerts to your phone or chat
		</a>
	</div>
{/if}
