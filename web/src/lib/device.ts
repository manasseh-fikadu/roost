// How the UI shows a device.

import Cpu from '@lucide/svelte/icons/cpu';
import Laptop from '@lucide/svelte/icons/laptop';
import Monitor from '@lucide/svelte/icons/monitor';
import Server from '@lucide/svelte/icons/server';
import Smartphone from '@lucide/svelte/icons/smartphone';
import Tablet from '@lucide/svelte/icons/tablet';
import type { Device, DeviceKind } from './api/types.js';
import type { Icon } from './icon.js';

const kindIcons: Record<DeviceKind, Icon> = {
	server: Server,
	laptop: Laptop,
	desktop: Monitor,
	tablet: Tablet,
	phone: Smartphone,
	other: Cpu
};

/** An icon for a device, from its kind. */
export function deviceIcon(d: Device): Icon {
	return kindIcons[d.kind];
}

const kindLabels: Record<DeviceKind, string> = {
	server: 'Server',
	laptop: 'Laptop',
	desktop: 'Desktop',
	tablet: 'Tablet',
	phone: 'Phone',
	other: 'Other'
};

/** The kinds of device, in the order the hub lists them. */
export const deviceKinds = Object.keys(kindLabels) as DeviceKind[];

/** A kind of device's name, such as Laptop. */
export function kindLabel(kind: DeviceKind): string {
	return kindLabels[kind];
}

/** The first part of a device's DNS name, such as laptop for laptop.example.ts.net. */
export function shortName(d: Device): string {
	return d.dnsName.split('.')[0];
}
