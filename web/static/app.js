// Flock - Agent Orchestration UI
(function () {
    'use strict';

    // =========================================================================
    // Section 1 — State
    // =========================================================================

    const store = {
        instances: new Map(),
        sessions: new Map(),
        messages: new Map(),       // msgID -> message
        streamingParts: new Map(), // partID -> accumulated part

        selectedInstanceId: null,
        selectedSessionId: null,
        sessionBusy: false,
        sessionBusyTool: null,
        eventSource: null,
        _instanceHash: '',
        _lastSentText: null,  // track user message to filter streaming echoes
    };

    // =========================================================================
    // Section 2 — DOM Helpers
    // =========================================================================

    function reconcileList(container, items, keyFn, createFn, updateFn) {
        const existing = new Map();
        for (const child of Array.from(container.children)) {
            const k = child.dataset.key;
            if (k) existing.set(k, child);
        }
        const fragment = document.createDocumentFragment();
        const seen = new Set();
        for (const item of items) {
            const key = keyFn(item);
            seen.add(key);
            let el = existing.get(key);
            if (el) { updateFn(el, item); } else { el = createFn(item); el.dataset.key = key; }
            fragment.appendChild(el);
        }
        for (const [key, el] of existing) { if (!seen.has(key)) el.remove(); }
        container.textContent = '';
        container.appendChild(fragment);
    }

    function h(tag, attrs, ...children) {
        const el = document.createElement(tag);
        if (attrs) {
            for (const [k, v] of Object.entries(attrs)) {
                if (k === 'className') el.className = v;
                else if (k === 'textContent') el.textContent = v;
                else if (k === 'innerHTML') el.innerHTML = v;
                else if (k.startsWith('on') && typeof v === 'function') el.addEventListener(k.slice(2).toLowerCase(), v);
                else el.setAttribute(k, v);
            }
        }
        for (const c of children) {
            if (typeof c === 'string') el.appendChild(document.createTextNode(c));
            else if (c) el.appendChild(c);
        }
        return el;
    }

    function esc(s) { const d = document.createElement('div'); d.textContent = s || ''; return d.innerHTML; }

    // =========================================================================
    // Section 3 — API
    // =========================================================================

    async function api(method, path, body) {
        const opts = { method, headers: { 'Content-Type': 'application/json' } };
        if (body) opts.body = JSON.stringify(body);
        const resp = await fetch(path, opts);
        if (!resp.ok) throw new Error(`${resp.status}: ${await resp.text()}`);
        if (resp.status === 204) return null;
        return resp.json();
    }

    async function refreshInstances() {
        try {
            const list = await api('GET', '/api/instances') || [];
            const hash = JSON.stringify(list.map(i => [i.id, i.status, i.working_directory]));
            if (hash === store._instanceHash) return;
            store._instanceHash = hash;
            store.instances.clear();
            for (const inst of list) store.instances.set(inst.id, inst);
            renderInstances();
        } catch (e) { console.error('refreshInstances:', e); }
    }

    async function createInstance(workDir) {
        try {
            await api('POST', '/api/instances', { working_directory: workDir });
            store._instanceHash = '';
            await refreshInstances();
        } catch (e) { alert('Failed to create instance: ' + e.message); }
    }

    async function deleteInstance(id) {
        try { await api('DELETE', `/api/instances/${id}`); } catch (e) { /* ignore */ }
        if (store.selectedInstanceId === id) {
            store.selectedInstanceId = null;
            store.selectedSessionId = null;
            store.sessions.clear();
            store.messages.clear();
            store.streamingParts.clear();
            closeEventSource();
            renderSessions(); renderMessages(); updateHeader();
        }
        store._instanceHash = '';
        await refreshInstances();
    }

    async function restoreInstance(id) {
        try {
            await api('POST', `/api/instances/${id}/restore`);
            store._instanceHash = '';
            await refreshInstances();
        } catch (e) { alert('Failed to restore instance: ' + e.message); }
    }

    async function loadSessions(instanceId) {
        try {
            const list = await api('GET', `/api/instances/${instanceId}/sessions`) || [];
            store.sessions.clear();
            for (const s of list) store.sessions.set(s.id, s);
        } catch (e) { console.error('loadSessions:', e); store.sessions.clear(); }
        renderSessions();
    }

    async function createSession(instanceId) {
        const inst = store.instances.get(instanceId);
        if (inst && inst.status !== 'running') {
            alert(`Instance is ${inst.status}. Wait for it to finish starting.`);
            return;
        }
        try {
            const session = await api('POST', `/api/instances/${instanceId}/sessions`);
            await loadSessions(instanceId);
            selectSession(session.id);
        } catch (e) { alert('Failed to create session: ' + e.message); }
    }

    async function loadMessages(sessionId) {
        try {
            const msgs = await api('GET', `/api/sessions/${sessionId}/messages`) || [];
            if (store.selectedSessionId !== sessionId) return;

            // Always save optimistic messages first
            const optimistic = [];
            for (const [id, m] of store.messages) {
                if (m._optimistic) optimistic.push([id, m]);
            }

            store.messages.clear();

            // Re-add optimistic messages
            for (const [id, m] of optimistic) store.messages.set(id, m);

            // Add API messages
            for (const m of msgs) {
                if (!m.info?.id) continue;
                store.messages.set(m.info.id, m);
            }

            // Deduplicate: if API returned a user message with same text as optimistic,
            // remove the optimistic (API version has real ID/timestamp)
            for (const [optId, optMsg] of optimistic) {
                const optText = extractText(optMsg.parts).trim();
                if (!optText) continue;
                for (const m of msgs) {
                    if (m.info?.role === 'user' && extractText(m.parts).trim() === optText) {
                        store.messages.delete(optId);
                        break;
                    }
                }
            }

            store.streamingParts.clear();
            store._lastSentText = null;
            renderMessages();
        } catch (e) { console.error('loadMessages:', e); }
    }

    async function sendMessage(sessionId, content) {
        try {
            await api('POST', `/api/sessions/${sessionId}/messages`, { content });
        } catch (e) {
            console.error('sendMessage:', e);
            alert('Failed to send message: ' + e.message);
            store.sessionBusy = false;
            updateInputState();
        }
    }

    // =========================================================================
    // Section 4 — SSE
    // =========================================================================

    function closeEventSource() {
        if (store.eventSource) { store.eventSource.close(); store.eventSource = null; }
    }

    function connectSSE(sessionId) {
        closeEventSource();
        const es = new EventSource(`/api/sessions/${sessionId}/events`);
        store.eventSource = es;
        es.onmessage = function (e) {
            if (store.selectedSessionId !== sessionId) { es.close(); return; }
            try { routeEvent(JSON.parse(e.data)); } catch (err) { /* ignore */ }
        };
        es.onerror = function () { /* auto-reconnects */ };
    }

    function routeEvent(event) {
        const type = event.type;
        const props = event.properties || {};
        switch (type) {
            case 'message.part.delta': handlePartDelta(props); break;
            case 'message.part.updated': handlePartUpdated(props); break;
            case 'message.updated': handleMessageUpdated(props); break;
            case 'session.status': handleSessionStatus(props); break;
            case 'session.updated': handleSessionUpdated(props); break;
            case 'session.idle': handleSessionIdle(); break;
            default: console.debug('SSE:', type, props);
        }
    }

    function handlePartDelta(props) {
        const { partID, delta, field, messageID } = props;
        if (!partID || delta === undefined) return;
        if (!store.streamingParts.has(partID)) {
            store.streamingParts.set(partID, { _messageID: messageID });
        }
        const part = store.streamingParts.get(partID);
        if (field) part[field] = (part[field] || '') + delta;
        // Infer type from fields
        if (!part.type) {
            if (field === 'text') part.type = 'text';
            else if (field === 'toolName' || field === 'args') part.type = 'tool-invocation';
            else if (field === 'reasoning') part.type = 'reasoning';
        }
        renderStreamingArea();
    }

    function handlePartUpdated(props) {
        const part = props.part;
        if (!part) return;
        const id = part.id || part.partID;
        if (!id) return;
        const existing = store.streamingParts.get(id);
        store.streamingParts.set(id, { ...part, _messageID: part.messageID || (existing && existing._messageID) });
        renderStreamingArea();
    }

    function handleMessageUpdated(props) {
        const msg = props.message || props;
        if (msg && msg.info && msg.info.id) {
            store.messages.set(msg.info.id, msg);
            renderMessages();
        }
    }

    function handleSessionStatus(props) {
        const st = props.status?.type;
        if (st === 'busy') { store.sessionBusy = true; store.sessionBusyTool = props.status?.tool || null; }
        else if (st === 'idle') { store.sessionBusy = false; store.sessionBusyTool = null; }
        updateInputState(); updateHeaderStatus();
    }

    function handleSessionUpdated(props) {
        const info = props.info;
        if (!info) return;
        const sess = store.sessions.get(info.id);
        if (sess && info.title && info.title !== sess.title) {
            sess.title = info.title;
            store.sessions.set(info.id, sess);
            renderSessions();
            if (store.selectedSessionId === info.id) updateHeader();
        }
    }

    function handleSessionIdle() {
        store.sessionBusy = false;
        store.sessionBusyTool = null;
        updateInputState(); updateHeaderStatus();
        if (store.selectedSessionId) loadMessages(store.selectedSessionId);
    }

    // =========================================================================
    // Section 5 — Send (with optimistic user message)
    // =========================================================================

    function doSend() {
        const input = document.getElementById('message-input');
        const content = input.value.trim();
        if (!content || !store.selectedSessionId || store.sessionBusy) return;
        input.value = '';
        input.style.height = 'auto';

        store._lastSentText = content;

        const optId = '_opt_' + Date.now();
        store.messages.set(optId, {
            info: { id: optId, role: 'user', time: { created: Date.now() / 1000 } },
            parts: [{ type: 'text', text: content }],
            _optimistic: true,
        });
        renderMessages();
        scrollToBottom();

        store.sessionBusy = true;
        updateInputState();
        sendMessage(store.selectedSessionId, content);
    }

    // =========================================================================
    // Section 6 — Message Rendering
    // =========================================================================

    /** Normalize parts from json.RawMessage or objects */
    function normalizeParts(parts) {
        if (!parts || !Array.isArray(parts)) return [];
        const result = [];
        for (const p of parts) {
            if (p == null) continue;
            let parsed = p;
            if (typeof p === 'string') {
                try { parsed = JSON.parse(p); } catch { continue; }
            }
            if (parsed && typeof parsed === 'object') result.push(parsed);
        }
        return result;
    }

    function extractText(parts) {
        const texts = [];
        for (const p of normalizeParts(parts)) {
            if (p.type === 'text' && (p.text || p.content)) texts.push(p.text || p.content);
        }
        return texts.join('\n');
    }

    function renderMessages() {
        const settled = document.getElementById('settled-messages');
        if (!settled) return;

        const msgs = Array.from(store.messages.values());
        msgs.sort((a, b) => (a.info.time?.created || 0) - (b.info.time?.created || 0));

        const frag = document.createDocumentFragment();
        let i = 0;

        while (i < msgs.length) {
            const msg = msgs[i];
            const role = msg.info?.role || 'assistant';

            if (role === 'user') {
                const el = buildUserBubble(msg);
                if (el) frag.appendChild(el);
                i++;
            } else {
                // Group consecutive assistant messages into one bubble
                const group = [];
                while (i < msgs.length && (msgs[i].info?.role || 'assistant') !== 'user') {
                    group.push(msgs[i]);
                    i++;
                }
                const el = buildAssistantBubble(group);
                if (el) frag.appendChild(el);
            }
        }

        settled.textContent = '';
        if (!frag.childNodes.length && !store.streamingParts.size) {
            settled.innerHTML = '<div class="flex items-center justify-center text-gray-600 py-20"><p>No messages yet.</p></div>';
        } else {
            settled.appendChild(frag);
        }

        renderStreamingArea();
        scrollToBottom();
    }

    function buildUserBubble(msg) {
        const text = extractText(msg.parts);
        if (!text) return null;
        const wrapper = h('div', { className: 'flex justify-end' });
        wrapper.appendChild(h('div', {
            className: `max-w-3xl rounded-lg px-4 py-3 bg-blue-600 ${msg._optimistic ? 'opacity-60' : ''}`,
        }, h('pre', { className: 'whitespace-pre-wrap text-sm leading-relaxed', textContent: text })));
        return wrapper;
    }

    function buildAssistantBubble(messages) {
        // Collect all parts from all messages in the group, in order
        const allParts = [];
        for (const msg of messages) {
            for (const p of normalizeParts(msg.parts)) {
                allParts.push(p);
            }
        }
        if (!allParts.length) return null;

        // Filter and render parts
        const rendered = [];
        for (const part of allParts) {
            const el = renderPart(part);
            if (el) rendered.push(el);
        }
        if (!rendered.length) return null;

        const wrapper = h('div', { className: 'flex justify-start' });
        const bubble = h('div', {
            className: 'max-w-3xl rounded-lg px-4 py-3 bg-gray-800 border border-gray-700 space-y-2',
        });
        for (const el of rendered) bubble.appendChild(el);
        wrapper.appendChild(bubble);
        return wrapper;
    }

    // =========================================================================
    // Section 6b — Part Renderers
    // =========================================================================

    function renderPart(part) {
        if (!part || typeof part !== 'object') return null;
        const type = part.type || '';
        switch (type) {
            case 'text':        return renderTextPart(part);
            case 'tool':        return renderToolPart(part);     // OpenCode native format
            case 'tool-invocation': return renderToolInvPart(part); // AI SDK format
            case 'tool-result': return renderToolResultPart(part);
            case 'reasoning':   return renderReasoningPart(part);
            case 'step-start':  return null; // hidden
            case 'step-finish': return null; // hidden
            default:            return renderGenericPart(part);
        }
    }

    function renderTextPart(part) {
        const text = part.text || '';
        if (!text.trim()) return null;
        const div = h('div', { className: 'prose text-sm' });
        div.innerHTML = renderMarkdown(text);
        return div;
    }

    /**
     * Render OpenCode-native tool part.
     * Structure: { type:"tool", tool:"bash", callID, state:{ status, input, output, title, metadata, time } }
     */
    function renderToolPart(part) {
        const toolName = part.tool || 'tool';
        const state = part.state || {};
        const status = state.status || 'running';
        const title = state.title || state.input?.description || '';
        const input = state.input;
        const output = state.output || state.metadata?.output || '';

        return buildToolDetails(toolName, status, title, input, output);
    }

    /** Render AI-SDK-style tool-invocation part */
    function renderToolInvPart(part) {
        const toolName = part.toolName || part.name || 'tool';
        const status = part.state || part.toolState || 'running';
        const title = '';
        const input = part.args;
        const output = part.result;

        return buildToolDetails(toolName, status, title, input, output);
    }

    /** Shared tool rendering — compact Claude-Code style */
    function buildToolDetails(toolName, status, title, input, output) {
        const dotClass = status === 'completed' ? 'bg-green-400'
            : status === 'error' ? 'bg-red-400'
            : 'bg-yellow-400 animate-pulse';

        const details = h('details', { className: 'my-1 group' });

        // Summary line: ● toolName — title
        const summary = h('summary', {
            className: 'flex items-center gap-2 cursor-pointer text-sm text-gray-300 hover:text-gray-100 select-none list-none py-0.5',
        });
        summary.appendChild(h('span', { className: `w-2 h-2 rounded-full ${dotClass} flex-shrink-0` }));
        summary.appendChild(h('code', { className: 'text-xs font-mono text-gray-300', textContent: toolName }));
        if (title) {
            summary.appendChild(h('span', { className: 'text-gray-500', textContent: '—' }));
            summary.appendChild(h('span', { className: 'text-xs text-gray-400 truncate', textContent: title }));
        }
        if (status === 'running') {
            summary.appendChild(h('span', { className: 'text-xs text-yellow-500 animate-pulse', textContent: 'running…' }));
        }
        details.appendChild(summary);

        // Expandable body
        const body = h('div', { className: 'mt-1.5 ml-4 space-y-1.5 text-xs' });

        if (input) {
            const inputStr = typeof input === 'string' ? input : JSON.stringify(input, null, 2);
            if (inputStr && inputStr !== '{}' && inputStr !== 'null') {
                const pre = h('pre', { className: 'bg-gray-900 rounded p-2 overflow-x-auto max-h-40 overflow-y-auto text-gray-400 custom-scrollbar' });
                pre.appendChild(h('code', { textContent: inputStr }));
                body.appendChild(pre);
            }
        }

        if (output) {
            const outputStr = typeof output === 'string' ? output : JSON.stringify(output, null, 2);
            if (outputStr) {
                body.appendChild(h('div', { className: 'text-gray-500', textContent: 'Output:' }));
                const pre = h('pre', { className: 'bg-gray-900 rounded p-2 overflow-x-auto max-h-48 overflow-y-auto text-gray-400 custom-scrollbar' });
                pre.appendChild(h('code', { textContent: outputStr }));
                body.appendChild(pre);
            }
        }

        if (body.childNodes.length) details.appendChild(body);
        return details;
    }

    function renderToolResultPart(part) {
        const result = part.result || part.text || part.content || '';
        if (!result) return null;
        const str = typeof result === 'string' ? result : JSON.stringify(result, null, 2);
        const details = h('details', { className: 'my-1' });
        const summary = h('summary', {
            className: 'flex items-center gap-2 cursor-pointer text-sm text-gray-400 hover:text-gray-200 select-none list-none py-0.5',
        });
        summary.appendChild(h('span', { className: 'w-2 h-2 rounded-full bg-gray-500 flex-shrink-0' }));
        summary.appendChild(h('span', { className: 'text-xs', textContent: 'Tool Result' }));
        details.appendChild(summary);
        const pre = h('pre', { className: 'mt-1.5 ml-4 bg-gray-900 rounded p-2 overflow-x-auto max-h-48 overflow-y-auto text-xs text-gray-400 custom-scrollbar' });
        pre.appendChild(h('code', { textContent: str }));
        details.appendChild(pre);
        return details;
    }

    function renderReasoningPart(part) {
        const text = part.reasoning || part.text || '';
        if (!text.trim()) return null;
        const details = h('details', { className: 'my-1' });
        details.appendChild(h('summary', {
            className: 'cursor-pointer text-sm text-gray-500 hover:text-gray-300 italic select-none list-none py-0.5',
            textContent: 'Thinking…',
        }));
        details.appendChild(h('div', {
            className: 'mt-1 ml-4 text-sm text-gray-400 italic whitespace-pre-wrap',
            textContent: text,
        }));
        return details;
    }

    function renderGenericPart(part) {
        const json = JSON.stringify(part, null, 2);
        if (!json || json === '{}' || json === 'null') return null;
        // Skip parts that are just metadata (sessionID, messageID, id only)
        const keys = Object.keys(part).filter(k => !['id', 'sessionID', 'messageID', 'type'].includes(k));
        if (!keys.length) return null;
        const details = h('details', { className: 'my-1' });
        details.appendChild(h('summary', {
            className: 'cursor-pointer text-xs text-gray-600 hover:text-gray-400 select-none list-none',
            textContent: `[${part.type || 'unknown'}]`,
        }));
        const pre = h('pre', { className: 'mt-1 ml-4 bg-gray-900 rounded p-2 overflow-x-auto max-h-32 overflow-y-auto text-xs text-gray-500 custom-scrollbar' });
        pre.appendChild(h('code', { textContent: json }));
        details.appendChild(pre);
        return details;
    }

    // =========================================================================
    // Section 7 — Streaming Area
    // =========================================================================

    function renderStreamingArea() {
        const container = document.getElementById('streaming-area');
        if (!container) return;

        if (!store.streamingParts.size) {
            container.innerHTML = '';
            container.classList.add('hidden');
            return;
        }
        container.classList.remove('hidden');

        // Categorize streaming parts
        const reasoning = [];
        const tools = [];
        const texts = [];

        for (const [, part] of store.streamingParts) {
            const type = part.type || '';
            if (type === 'reasoning' || part.reasoning) reasoning.push(part);
            else if (type === 'tool-invocation' || type === 'tool' || part.toolName || part.tool) tools.push(part);
            else if (type === 'text' || part.text) {
                // Skip if this is an echo of the user's sent message
                if (store._lastSentText && part.text && part.text.trim() === store._lastSentText.trim()) continue;
                texts.push(part);
            }
        }

        const bubble = h('div', {
            className: 'max-w-3xl rounded-lg px-4 py-3 bg-gray-800 border border-gray-700 border-dashed space-y-2',
        });

        // Reasoning
        for (const part of reasoning) {
            const text = part.reasoning || part.text || '';
            if (!text) continue;
            const d = h('details', { className: 'my-1' });
            d.setAttribute('open', '');
            d.appendChild(h('summary', {
                className: 'cursor-pointer text-sm text-gray-500 italic select-none list-none',
                textContent: 'Thinking…',
            }));
            d.appendChild(h('div', { className: 'mt-1 text-sm text-gray-400 italic whitespace-pre-wrap', textContent: text }));
            bubble.appendChild(d);
        }

        // Tool calls
        for (const part of tools) {
            const toolName = part.toolName || part.tool || part.name || 'tool';
            const el = buildToolDetails(toolName, part.state?.status || part.state || 'running', '', part.args || part.state?.input, part.result || part.state?.output);
            if (el) bubble.appendChild(el);
        }

        // Text with cursor
        const allText = texts.map(p => p.text || '').join('');
        if (allText) {
            const div = h('div', { className: 'prose text-sm' });
            div.innerHTML = renderMarkdown(allText);
            div.appendChild(h('span', { className: 'inline-block w-1.5 h-4 bg-blue-400 animate-pulse ml-0.5 align-text-bottom' }));
            bubble.appendChild(div);
        } else if (!tools.length && !reasoning.length) {
            bubble.appendChild(h('span', { className: 'inline-block w-1.5 h-4 bg-blue-400 animate-pulse' }));
        }

        container.innerHTML = '';
        container.appendChild(h('div', { className: 'flex justify-start' }, bubble));
        scrollToBottom();
    }

    // =========================================================================
    // Section 8 — Sidebar
    // =========================================================================

    function renderInstances() {
        const list = document.getElementById('instance-list');
        if (!list) return;
        const items = Array.from(store.instances.values());
        if (!items.length) { list.innerHTML = '<p class="text-xs text-gray-500 py-2">No instances running</p>'; return; }
        reconcileList(list, items, i => i.id, createInstanceEl, updateInstanceEl);
    }

    function createInstanceEl(inst) {
        const sel = inst.id === store.selectedInstanceId;
        const stopped = inst.status === 'stopped';
        const div = h('div', {
            className: `flex items-center gap-2 px-2 py-1.5 rounded cursor-pointer group ${sel ? 'bg-gray-700' : 'hover:bg-gray-800'}`,
            'data-action': 'select-instance', 'data-id': inst.id,
        });
        div.appendChild(h('span', { className: `w-2 h-2 rounded-full ${statusColor(inst.status)} flex-shrink-0` }));
        div.appendChild(h('span', { className: `text-sm truncate flex-1 ${stopped ? 'text-gray-500' : ''}`, textContent: inst.working_directory.split('/').pop() || inst.working_directory, title: inst.working_directory }));
        if (inst.port) div.appendChild(h('span', { className: 'text-xs text-gray-500 flex-shrink-0', textContent: ':' + inst.port }));
        if (stopped) {
            div.appendChild(h('button', {
                className: 'text-xs px-1.5 py-0.5 rounded bg-blue-600 hover:bg-blue-500 text-white opacity-0 group-hover:opacity-100 transition-opacity flex-shrink-0',
                'data-action': 'restore-instance', 'data-id': inst.id, textContent: 'Restore',
            }));
        }
        div.appendChild(h('button', {
            className: 'text-gray-500 hover:text-red-400 text-xs px-1 opacity-0 group-hover:opacity-100 transition-opacity flex-shrink-0',
            'data-action': 'delete-instance', 'data-id': inst.id, textContent: '\u00d7',
        }));
        return div;
    }

    function updateInstanceEl(el, inst) {
        const sel = inst.id === store.selectedInstanceId;
        el.className = `flex items-center gap-2 px-2 py-1.5 rounded cursor-pointer group ${sel ? 'bg-gray-700' : 'hover:bg-gray-800'}`;
        const dot = el.querySelector('span:first-child');
        if (dot) dot.className = `w-2 h-2 rounded-full ${statusColor(inst.status)} flex-shrink-0`;
    }

    function statusColor(s) {
        return { running: 'bg-green-500', starting: 'bg-yellow-500 animate-pulse', error: 'bg-red-500', stopped: 'bg-gray-500' }[s] || 'bg-gray-500';
    }

    function renderSessions() {
        const list = document.getElementById('session-list');
        if (!list) return;
        const items = Array.from(store.sessions.values());
        if (!items.length) { list.innerHTML = '<p class="text-xs text-gray-500 py-2">No sessions</p>'; return; }
        reconcileList(list, items, s => s.id, createSessionEl, updateSessionEl);
    }

    function createSessionEl(sess) {
        const sel = sess.id === store.selectedSessionId;
        const title = sess.title || 'Untitled';
        const busy = sel && store.sessionBusy;
        const div = h('div', {
            className: `flex items-center gap-2 px-2 py-1.5 rounded cursor-pointer text-sm ${sel ? 'bg-gray-700 text-white' : 'text-gray-400 hover:bg-gray-800'}`,
            'data-action': 'select-session', 'data-id': sess.id, title,
        });
        div.appendChild(h('span', { className: `w-1.5 h-1.5 rounded-full flex-shrink-0 ${busy ? 'bg-yellow-400 animate-pulse' : 'bg-gray-600'}` }));
        div.appendChild(h('span', { className: 'truncate', textContent: title }));
        return div;
    }

    function updateSessionEl(el, sess) {
        const sel = sess.id === store.selectedSessionId;
        const title = sess.title || 'Untitled';
        const busy = sel && store.sessionBusy;
        el.className = `flex items-center gap-2 px-2 py-1.5 rounded cursor-pointer text-sm ${sel ? 'bg-gray-700 text-white' : 'text-gray-400 hover:bg-gray-800'}`;
        el.title = title;
        const dot = el.querySelector('span:first-child');
        if (dot) dot.className = `w-1.5 h-1.5 rounded-full flex-shrink-0 ${busy ? 'bg-yellow-400 animate-pulse' : 'bg-gray-600'}`;
        const titleEl = el.querySelector('span:last-child');
        if (titleEl && titleEl.textContent !== title) titleEl.textContent = title;
    }

    // =========================================================================
    // Section 9 — Resize
    // =========================================================================

    function initResize() {
        const handle = document.getElementById('resize-handle');
        const sidebar = document.getElementById('sidebar');
        if (!handle || !sidebar) return;
        let startX, startW;
        handle.addEventListener('mousedown', (e) => {
            e.preventDefault();
            startX = e.clientX; startW = sidebar.offsetWidth;
            document.body.style.cursor = 'col-resize';
            document.body.style.userSelect = 'none';
            function onMove(e) { sidebar.style.width = Math.min(480, Math.max(224, startW + e.clientX - startX)) + 'px'; }
            function onUp() { document.removeEventListener('mousemove', onMove); document.removeEventListener('mouseup', onUp); document.body.style.cursor = ''; document.body.style.userSelect = ''; }
            document.addEventListener('mousemove', onMove);
            document.addEventListener('mouseup', onUp);
        });
    }

    // =========================================================================
    // Section 10 — Selection, Input, Events
    // =========================================================================

    function selectInstance(id) {
        store.selectedInstanceId = id;
        store.selectedSessionId = null;
        store.sessions.clear(); store.messages.clear(); store.streamingParts.clear();
        store.sessionBusy = false; store.sessionBusyTool = null;
        closeEventSource();
        renderInstances(); renderSessions(); renderMessages(); updateHeader(); updateHeaderStatus();
        document.getElementById('btn-new-session').classList.remove('hidden');
        document.getElementById('input-area').classList.add('hidden');
        loadSessions(id);
    }

    function selectSession(id) {
        if (store.selectedSessionId === id) return;
        store.selectedSessionId = id;
        store.messages.clear(); store.streamingParts.clear();
        store.sessionBusy = false; store.sessionBusyTool = null;
        renderSessions(); renderMessages(); updateHeader(); updateHeaderStatus(); updateInputState();
        document.getElementById('input-area').classList.remove('hidden');
        loadMessages(id);
        connectSSE(id);
    }

    function updateHeader() {
        const el = document.getElementById('main-header');
        if (!el) return;
        if (store.selectedSessionId) {
            el.textContent = store.sessions.get(store.selectedSessionId)?.title || 'Session';
        } else if (store.selectedInstanceId) {
            const inst = store.instances.get(store.selectedInstanceId);
            el.textContent = inst ? `Instance: ${inst.working_directory}` : 'Instance';
        } else { el.textContent = 'Select an instance to get started'; }
    }

    function updateHeaderStatus() {
        const el = document.getElementById('header-status');
        if (!el) return;
        if (!store.selectedSessionId) { el.innerHTML = ''; return; }
        if (store.sessionBusy) {
            const t = store.sessionBusyTool ? ` \u00b7 ${esc(store.sessionBusyTool)}` : '';
            el.innerHTML = `<span class="w-2 h-2 rounded-full bg-yellow-400 animate-pulse inline-block"></span><span>Busy${t}</span>`;
        } else {
            el.innerHTML = '<span class="w-2 h-2 rounded-full bg-green-500 inline-block"></span><span>Idle</span>';
        }
    }

    function updateInputState() {
        const btn = document.getElementById('btn-send');
        const input = document.getElementById('message-input');
        if (!btn || !input) return;
        btn.disabled = store.sessionBusy;
        btn.textContent = store.sessionBusy ? 'Working\u2026' : 'Send';
        input.disabled = store.sessionBusy;
        if (!store.sessionBusy) input.focus();
    }

    function scrollToBottom() {
        const el = document.getElementById('messages');
        if (el) requestAnimationFrame(() => { el.scrollTop = el.scrollHeight; });
    }

    // Event delegation
    document.addEventListener('click', function (e) {
        const t = e.target.closest('[data-action]');
        if (!t) return;
        switch (t.dataset.action) {
            case 'select-instance': selectInstance(t.dataset.id); break;
            case 'delete-instance': e.stopPropagation(); if (confirm('Stop this instance?')) deleteInstance(t.dataset.id); break;
            case 'restore-instance': e.stopPropagation(); restoreInstance(t.dataset.id); break;
            case 'select-session': selectSession(t.dataset.id); break;
        }
    });

    document.getElementById('btn-new-instance').addEventListener('click', () => {
        document.getElementById('modal-new-instance').classList.remove('hidden');
        document.getElementById('input-workdir').value = '';
        document.getElementById('input-workdir').focus();
    });
    document.getElementById('btn-cancel-instance').addEventListener('click', () => document.getElementById('modal-new-instance').classList.add('hidden'));
    document.getElementById('btn-create-instance').addEventListener('click', () => {
        const wd = document.getElementById('input-workdir').value.trim();
        if (!wd) return;
        document.getElementById('modal-new-instance').classList.add('hidden');
        createInstance(wd);
    });
    document.getElementById('input-workdir').addEventListener('keydown', (e) => {
        if (e.key === 'Enter') { e.preventDefault(); document.getElementById('btn-create-instance').click(); }
    });
    document.getElementById('btn-new-session').addEventListener('click', () => {
        if (store.selectedInstanceId) createSession(store.selectedInstanceId);
    });
    document.getElementById('message-input').addEventListener('keydown', (e) => {
        if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); doSend(); }
    });
    document.getElementById('btn-send').addEventListener('click', (e) => { e.preventDefault(); doSend(); });
    document.getElementById('message-input').addEventListener('input', function () {
        this.style.height = 'auto';
        this.style.height = Math.min(this.scrollHeight, 200) + 'px';
    });
    document.getElementById('modal-new-instance').addEventListener('click', (e) => {
        if (e.target === e.currentTarget) e.currentTarget.classList.add('hidden');
    });
    document.addEventListener('keydown', (e) => {
        if (e.key === 'Escape') document.getElementById('modal-new-instance').classList.add('hidden');
    });

    // =========================================================================
    // Section 11 — Markdown
    // =========================================================================

    let markdownReady = false;

    function initMarkdown() {
        if (typeof marked === 'undefined') { setTimeout(initMarkdown, 100); return; }
        const renderer = new marked.Renderer();
        renderer.code = function ({ text, lang }) {
            if (typeof hljs !== 'undefined' && lang && hljs.getLanguage(lang)) {
                try { return `<pre class="hljs rounded-lg overflow-x-auto"><code class="language-${esc(lang)}">${hljs.highlight(text, { language: lang }).value}</code></pre>`; } catch (e) { /* fall through */ }
            }
            return `<pre class="hljs rounded-lg overflow-x-auto"><code>${esc(text)}</code></pre>`;
        };
        renderer.codespan = function (token) {
            // Handle both marked v12 token objects and older string args
            const t = typeof token === 'string' ? token : (token.text ?? token.raw ?? '');
            // marked v12 already HTML-escapes text, so use directly
            return `<code class="bg-gray-700 text-gray-200 px-1.5 py-0.5 rounded text-sm">${t}</code>`;
        };
        marked.setOptions({ renderer, gfm: true, breaks: true });
        markdownReady = true;
    }

    function renderMarkdown(text) {
        if (!text) return '';
        if (markdownReady && typeof marked !== 'undefined') {
            try { return marked.parse(text); } catch (e) { console.error('md:', e); }
        }
        return '<pre class="whitespace-pre-wrap">' + esc(text) + '</pre>';
    }

    // =========================================================================
    // Init
    // =========================================================================
    initMarkdown();
    initResize();
    refreshInstances();
    setInterval(refreshInstances, 5000);
})();
