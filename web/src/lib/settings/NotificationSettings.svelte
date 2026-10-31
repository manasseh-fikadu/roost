<!-- The Notifications tab: the channels, and when Roost sends to them. -->
<script lang="ts">
	import type { SettingsView } from '#lib/api/types.js';
	import ChannelsCard from './ChannelsCard.svelte';
	import NotificationRules from './NotificationRules.svelte';

	interface Props {
		view: SettingsView;
		onsaved: (view: SettingsView) => void;
		/** Whether the tab is showing. */
		active: boolean;
	}

	let { view, onsaved, active }: Props = $props();
</script>

<div class="space-y-4">
	<ChannelsCard {active} />
	<NotificationRules {view} {onsaved} />
</div>
