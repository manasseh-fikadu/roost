<!--
	One terminal: xterm.js on a WebSocket to a shell on a device. Output
	arrives in binary frames, and the view acks it as xterm.js writes it to
	the screen, so a flood of output can't outrun the browser. Call
	reconnect() to open a new connection, such as to reattach after one drops.
-->
<script lang="ts">
	import { onMount } from 'svelte';
	import { FitAddon } from '@xterm/addon-fit';
	import { WebLinksAddon } from '@xterm/addon-web-links';
	import { Terminal } from '@xterm/xterm';
	import '@xterm/xterm/css/xterm.css';
	import type { TerminalMessage } from '#lib/api/types.js';
	import { Acker, terminalURL, type TermStatus } from '#lib/terminal.js';

	interface Props {
		deviceId: string;
		/** The tmux session to attach to, or null for a shell that ends with the connection. */
		session: string | null;
		/** Where the shell starts, or null for the user's home. */
		cwd: string | null;
		/** Whether the tab is showing, so the terminal fits and takes focus. */
		active: boolean;
		onstatus: (status: TermStatus) => void;
		/** Gets the title the shell sets, such as the current command. */
		ontitle?: (title: string) => void;
	}

	let { deviceId, session, cwd, active, onstatus, ontitle }: Props = $props();

	const font = '"Geist Mono Variable", ui-monospace, SFMono-Regular, Menlo, monospace';

	// Roost's palette, so the terminal looks like part of the page.
	const theme = {
		background: '#0a0a0a',
		foreground: '#ededed',
		cursor: '#ededed',
		cursorAccent: '#0a0a0a',
		selectionBackground: 'rgba(82, 168, 255, 0.35)',
		black: '#1f1f1f',
		red: '#ff6166',
		green: '#62c073',
		yellow: '#f1a10d',
		blue: '#52a8ff',
		magenta: '#bf7af0',
		cyan: '#4fd1c5',
		white: '#d4d4d4',
		brightBlack: '#5c5c5c',
		brightRed: '#ff8589',
		brightGreen: '#7fd48f',
		brightYellow: '#ffc24d',
		brightBlue: '#7cbdff',
		brightMagenta: '#d29cf5',
		brightCyan: '#76e4d8',
		brightWhite: '#ffffff'
	};

	let host: HTMLDivElement;
	let term: Terminal | undefined;
	let fit: FitAddon | undefined;
	let ws: WebSocket | undefined;

	function sendMessage(m: TerminalMessage) {
		if (ws?.readyState === WebSocket.OPEN) {
			ws.send(JSON.stringify(m));
		}
	}

	/** Types data into the terminal, as if from the keyboard. */
	export function send(data: string) {
		sendMessage({ t: 'i', d: data });
		term?.focus();
	}

	export function focus() {
		term?.focus();
	}

	/** Opens a new connection, closing the one there is. */
	export function reconnect() {
		const t = term;
		if (!t) {
			return;
		}
		ws?.close();
		onstatus({ state: 'connecting' });
		const sock = new WebSocket(
			terminalURL(location, deviceId, { session, cwd, cols: t.cols, rows: t.rows })
		);
		sock.binaryType = 'arraybuffer';
		ws = sock;
		const acker = new Acker();
		let failed = false;
		sock.onmessage = (ev: MessageEvent<ArrayBuffer | string>) => {
			if (typeof ev.data !== 'string') {
				const bytes = new Uint8Array(ev.data);
				t.write(bytes, () => {
					const n = acker.wrote(bytes.length);
					if (n > 0 && ws === sock) {
						sendMessage({ t: 'a', n });
					}
				});
				return;
			}
			const m: TerminalMessage = JSON.parse(ev.data);
			switch (m.t) {
				case 'ready':
					onstatus({ state: 'open' });
					// The size may have changed while the shell started.
					sendMessage({ t: 'r', c: t.cols, r: t.rows });
					break;
				case 'error':
					failed = true;
					onstatus({ state: 'error', message: m.message });
					break;
				case 'exit':
					onstatus({
						state: 'closed',
						message: session
							? `The tmux session ${session} ended.`
							: `The shell exited${m.code != null && m.code >= 0 ? ` with status ${m.code}` : ''}.`
					});
					break;
			}
		};
		sock.onclose = (ev) => {
			if (ws !== sock || failed || ev.code === 1000) {
				// A normal close follows an exit or an error, which said why.
				return;
			}
			onstatus({
				state: 'closed',
				message:
					ev.code === 1001
						? "Roost is restarting. Reattach once it's back."
						: "The connection dropped, or Roost couldn't open it."
			});
		};
	}

	onMount(() => {
		const t = new Terminal({
			fontFamily: font,
			fontSize: 13,
			lineHeight: 1.25,
			cursorBlink: true,
			scrollback: 10_000,
			macOptionIsMeta: true,
			theme
		});
		const f = new FitAddon();
		t.loadAddon(f);
		t.loadAddon(new WebLinksAddon((_event, uri) => window.open(uri, '_blank', 'noopener')));
		// Ctrl+Shift+C copies, since Ctrl+C has to reach the shell.
		t.attachCustomKeyEventHandler((e) => {
			if (e.type === 'keydown' && e.ctrlKey && e.shiftKey && e.code === 'KeyC') {
				if (t.hasSelection()) {
					navigator.clipboard?.writeText(t.getSelection()).catch(() => {});
				}
				return false;
			}
			return true;
		});
		t.onData((d) => sendMessage({ t: 'i', d }));
		t.onResize(({ cols, rows }) => sendMessage({ t: 'r', c: cols, r: rows }));
		t.onTitleChange((title) => ontitle?.(title));
		term = t;
		fit = f;

		let frame = 0;
		// A hidden tab measures 0×0, and fitting it then would shrink the shell.
		const refit = () => {
			cancelAnimationFrame(frame);
			frame = requestAnimationFrame(() => {
				if (host.offsetWidth > 0 && host.offsetHeight > 0) {
					f.fit();
				}
			});
		};
		const observer = new ResizeObserver(refit);
		let disposed = false;
		// Measuring the grid before the font loads gets its cells wrong.
		document.fonts
			.load(`13px ${font}`)
			.catch(() => {})
			.then(() => {
				if (disposed) {
					return;
				}
				t.open(host);
				if (host.offsetWidth > 0) {
					f.fit();
				}
				observer.observe(host);
				reconnect();
			});

		return () => {
			disposed = true;
			cancelAnimationFrame(frame);
			observer.disconnect();
			const sock = ws;
			ws = undefined;
			sock?.close();
			t.dispose();
			term = undefined;
		};
	});

	$effect(() => {
		if (!active) {
			return;
		}
		const frame = requestAnimationFrame(() => {
			if (host.offsetWidth > 0) {
				fit?.fit();
			}
			term?.focus();
		});
		return () => cancelAnimationFrame(frame);
	});
</script>

<div bind:this={host} class="size-full"></div>
