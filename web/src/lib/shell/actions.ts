// What the command palette can do besides jump to a page: open a terminal on
// a machine, restart a container, run a check. This works out the list from
// the live snapshot, and the palette turns each into a button.

import type { Snapshot } from '#lib/api/types.js';
import { actionsFor } from '#lib/docker/docker.js';
import type { DockerAction } from '#lib/docker/api.js';
import { canOpen, terminalDevices } from '#lib/terminal.js';

interface Base {
	/** Unique among the actions. */
	id: string;
	label: string;
	/** A few words after the label, such as the container's image. */
	hint: string;
	/** What the search matches, besides the label. */
	keywords: string[];
}

export type PaletteAction =
	| (Base & { kind: 'terminal' | 'files' | 'docker'; deviceId: string })
	| (Base & {
			kind: 'container';
			deviceId: string;
			deviceName: string;
			containerId: string;
			containerName: string;
			action: DockerAction;
	  })
	| (Base & { kind: 'run-check'; checkId: string })
	| (Base & { kind: 'ack-alert'; alertId: number })
	| (Base & { kind: 'new-check' })
	| (Base & { kind: 'shortcuts' });

const verbs: Record<DockerAction, string> = { start: 'Start', stop: 'Stop', restart: 'Restart' };

/**
 * The actions for what's in `snapshot`. Anyone can see the shortcuts and open
 * a machine's Docker. The rest is for admins, who are the only ones the hub
 * tells about SSH access, and the only ones the routes behind them accept.
 */
export function paletteActions(snapshot: Snapshot | null, admin: boolean): PaletteAction[] {
	const out: PaletteAction[] = [
		{
			id: 'shortcuts',
			kind: 'shortcuts',
			label: 'Show keyboard shortcuts',
			hint: '?',
			keywords: ['help', 'keys', 'keyboard', 'shortcuts']
		}
	];
	if (!snapshot) {
		return out;
	}
	const devices = snapshot.devices;
	for (const d of devices.filter((d) => d.docker)) {
		out.push({
			id: `docker:${d.id}`,
			kind: 'docker',
			deviceId: d.id,
			label: `Open Docker on ${d.name}`,
			hint: '',
			keywords: [d.name, d.dnsName, 'docker', 'containers']
		});
	}
	if (!admin) {
		return out;
	}
	for (const d of terminalDevices(devices).filter(canOpen)) {
		const keywords = [d.name, d.dnsName, ...d.ips];
		out.push(
			{
				id: `terminal:${d.id}`,
				kind: 'terminal',
				deviceId: d.id,
				label: `Open terminal on ${d.name}`,
				hint: '',
				keywords: [...keywords, 'terminal', 'shell', 'ssh']
			},
			{
				id: `files:${d.id}`,
				kind: 'files',
				deviceId: d.id,
				label: `Browse files on ${d.name}`,
				hint: '',
				keywords: [...keywords, 'files', 'sftp', 'folders']
			}
		);
	}
	for (const c of snapshot.containers) {
		const deviceName = devices.find((d) => d.id === c.deviceId)?.name ?? '';
		for (const action of actionsFor(c.state)) {
			out.push({
				id: `container:${c.deviceId}:${c.id}:${action}`,
				kind: 'container',
				deviceId: c.deviceId,
				deviceName,
				containerId: c.id,
				containerName: c.name,
				action,
				label: `${verbs[action]} ${c.name}`,
				hint: c.image,
				keywords: [c.name, c.image, 'container', 'docker', ...(c.project ? [c.project] : [])]
			});
		}
	}
	for (const c of snapshot.checks.filter((c) => !c.deviceId)) {
		out.push({
			id: `check:${c.id}`,
			kind: 'run-check',
			checkId: c.id,
			label: `Run check ${c.name}`,
			hint: '',
			keywords: [c.name, 'check', 'test', 'probe']
		});
	}
	for (const a of snapshot.alerts.filter((a) => a.state === 'open' && !a.ackedAt)) {
		out.push({
			id: `alert:${a.id}`,
			kind: 'ack-alert',
			alertId: a.id,
			label: `Acknowledge ${a.title}`,
			hint: '',
			keywords: [a.title, 'alert', 'ack', 'acknowledge']
		});
	}
	out.push({
		id: 'new-check',
		kind: 'new-check',
		label: 'Add a check',
		hint: '',
		keywords: ['check', 'new', 'monitor', 'website', 'port', 'dns']
	});
	return out;
}
