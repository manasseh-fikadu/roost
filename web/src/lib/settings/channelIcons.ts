// The icon of each kind of notification channel.

import BellRing from '@lucide/svelte/icons/bell-ring';
import Hash from '@lucide/svelte/icons/hash';
import Mail from '@lucide/svelte/icons/mail';
import MessagesSquare from '@lucide/svelte/icons/messages-square';
import Send from '@lucide/svelte/icons/send';
import Webhook from '@lucide/svelte/icons/webhook';
import type { ChannelKind } from '#lib/api/types.js';
import type { Icon } from '#lib/icon.js';

export const channelIcons: Record<ChannelKind, Icon> = {
	ntfy: BellRing,
	discord: MessagesSquare,
	slack: Hash,
	telegram: Send,
	email: Mail,
	webhook: Webhook
};
