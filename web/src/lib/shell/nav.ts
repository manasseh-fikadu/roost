// The sidebar's pages, in the order #22 settled on. Agents live under
// Devices, and services are split between Docker (compose projects and
// containers) and each device's page (systemd units).

import Activity from '@lucide/svelte/icons/activity';
import Bell from '@lucide/svelte/icons/bell';
import Container from '@lucide/svelte/icons/container';
import FolderOpen from '@lucide/svelte/icons/folder-open';
import HeartPulse from '@lucide/svelte/icons/heart-pulse';
import LayoutGrid from '@lucide/svelte/icons/layout-grid';
import Monitor from '@lucide/svelte/icons/monitor';
import Settings from '@lucide/svelte/icons/settings';
import SquareTerminal from '@lucide/svelte/icons/square-terminal';
import type { Icon } from '#lib/icon.js';

export interface NavItem {
	/** The page's route, which is also its path. */
	href:
		| '/'
		| '/devices'
		| '/alerts'
		| '/checks'
		| '/docker'
		| '/terminal'
		| '/files'
		| '/activity'
		| '/settings';
	label: string;
	icon: Icon;
	/** Only admins see the page in the sidebar, and only they can open it. */
	adminOnly?: boolean;
}

export interface NavSection {
	label: string;
	items: NavItem[];
}

export const nav: NavSection[] = [
	{
		label: 'Monitor',
		items: [
			{ href: '/', label: 'Overview', icon: LayoutGrid },
			{ href: '/devices', label: 'Devices', icon: Monitor },
			{ href: '/alerts', label: 'Alerts', icon: Bell },
			{ href: '/checks', label: 'Checks', icon: HeartPulse }
		]
	},
	{ label: 'Services', items: [{ href: '/docker', label: 'Docker', icon: Container }] },
	{
		label: 'Access',
		items: [
			{ href: '/terminal', label: 'Terminal', icon: SquareTerminal, adminOnly: true },
			{ href: '/files', label: 'Files', icon: FolderOpen, adminOnly: true }
		]
	},
	{
		label: 'System',
		items: [
			{ href: '/activity', label: 'Activity', icon: Activity },
			{ href: '/settings', label: 'Settings', icon: Settings, adminOnly: true }
		]
	}
];

/** The sections a caller sees: all of them for admins, and no admin pages for anyone else. */
export function navFor(admin: boolean): NavSection[] {
	return nav
		.map((s) => ({ ...s, items: s.items.filter((i) => admin || !i.adminOnly) }))
		.filter((s) => s.items.length > 0);
}

/** The pages a caller sees in the order of the sidebar, so the nth is the one numbered n in it. */
export function pagesFor(admin: boolean): NavItem[] {
	return navFor(admin).flatMap((s) => s.items);
}

/** The sidebar item a path belongs to, such as Devices for /devices/abc. */
export function navItemFor(pathname: string): NavItem | undefined {
	let best: NavItem | undefined;
	for (const item of nav.flatMap((s) => s.items)) {
		const matches =
			item.href === '/'
				? pathname === '/'
				: pathname === item.href || pathname.startsWith(`${item.href}/`);
		if (matches && (!best || item.href.length > best.href.length)) {
			best = item;
		}
	}
	return best;
}
