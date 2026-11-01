// What the caller may do, from GET /api/me. The server enforces all of it;
// these helpers only decide what the UI offers.

import {
	ReasonNoUser,
	ReasonRoleLookupFailed,
	ReasonBieldMachine,
	ReasonTaggedDevice,
	ReasonUnknownDevice,
	ReasonWhoIsFailed,
	RoleAdmin,
	RoleViewer,
	type Me,
	type Reason,
	type Role
} from './api/types.js';

/** Whether the caller can do everything in Bield. */
export function isAdmin(me: Me | null | undefined): boolean {
	return me?.role === RoleAdmin;
}

/** Whether the caller can view Bield at all, as an admin or a viewer. */
export function canView(me: Me | null | undefined): boolean {
	return me?.role === RoleAdmin || me?.role === RoleViewer;
}

const roleLabels: Record<Role, string> = {
	[RoleAdmin]: 'Admin',
	[RoleViewer]: 'Viewer'
};

/** The name of a role, for showing to people. */
export function roleLabel(role: Role): string {
	return roleLabels[role];
}

const reasons: Record<Reason, string> = {
	[ReasonUnknownDevice]: "This device isn't one Tailscale knows on Bield's tailnet.",
	[ReasonTaggedDevice]:
		"This device is tagged, so it belongs to no one. Open Bield from a device you're logged in to.",
	[ReasonNoUser]: 'Tailscale knows this device, but not who uses it.',
	[ReasonBieldMachine]:
		"You're on the machine Bield runs on. Any program here could connect to Bield, so Bield signs no one in from it. Open Bield from another device.",
	[ReasonWhoIsFailed]: "Bield couldn't ask Tailscale who you are. Try again in a moment.",
	[ReasonRoleLookupFailed]: "Bield couldn't look up your role. Try again in a moment."
};

function isReason(reason: string | undefined): reason is Reason {
	return reason !== undefined && Object.hasOwn(reasons, reason);
}

/**
 * Whether Bield couldn't find out who the caller is or what their role is,
 * this time. Asking again may work.
 */
export function lookupFailed(me: Me): boolean {
	return me.reason === ReasonWhoIsFailed || me.reason === ReasonRoleLookupFailed;
}

/** Why the caller can't view Bield, in a sentence or two, or "" if they can. */
export function whyCantView(me: Me): string {
	if (canView(me)) {
		return '';
	}
	if (me.signedIn && !lookupFailed(me)) {
		const as = me.login ? ` as ${me.login}` : '';
		return `You're signed in${as}, but only Bield's admins and viewers can view it. Ask an admin to add you.`;
	}
	return isReason(me.reason) ? reasons[me.reason] : "Bield couldn't tell who you are.";
}

/**
 * The command an admin can run on Bield's machine to make a signed-in caller
 * without a role a viewer, or undefined if that wouldn't help.
 */
export function addViewerCommand(me: Me): string | undefined {
	if (canView(me) || !me.signedIn || !me.login || lookupFailed(me)) {
		return undefined;
	}
	return `bield admin add-viewer ${shellWord(me.login)}`;
}

/**
 * Quotes s for a POSIX shell, unless it's plain. The caller chose their own
 * login, and an admin may paste this command, so a login like
 * `a;reboot@example.com` must stay one word.
 */
function shellWord(s: string): string {
	return /^[\w@.+-]+$/.test(s) ? s : `'${s.replaceAll("'", "'\\''")}'`;
}
