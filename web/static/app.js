// Flock - Agent Orchestration UI
(function () {
    'use strict';

    // =========================================================================
    // Section 1 — State Management
    // =========================================================================

    const store = {
        instances: new Map(),       // id -> instance object
        sessions: new Map(),        // id -> session object
        messages: new Map(),        // id -> message object
        streamingParts: new Map(),  // partID -> part object (accumulated)

        selectedInstanceId: null,
        selectedSessionId: null,
        sessionBusy: false,
        sessionBusyTool: null,      // name of active tool if any

        eventSource: null,

        // Track data hashes for diff-based rendering
        _instanceHash: '',
        _sessionHash: '',
    };

    // =========================================================================
    // Section 2 — DOM Helpers
    // =========================================================================

    /**
     * Keyed DOM reconciliation. Only adds/removes/reorders changed nodes.
     * - container: parent element
     * - items: array of data objects
     * - keyFn: function(item) -> unique string key
     * - createFn: function(item) -> new DOM element (must set data-key)
     * - updateFn: function(el, item) -> update existing element
     */
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
            if (el) {
                updateFn(el, item);
            } else {
                el = createFn(item);
                el.dataset.key = key;
            }
            fragment.appendChild(el);
        }

        // Remove nodes no longer in items
        for (const [key, el] of existing) {
            if (!seen.has(key)) el.remove();
        }

        container.appendChild(fragment);
    }

    /** Create an element with attributes and children */
    function h(tag, attrs, ...children) {
        const el = document.createElement(tag);
        if (attrs) {
            for (const [k, v] of Object.entries(attrs)) {
                if (k === 'className') el.className = v;
                else if (k === 'textContent') el.textContent = v;
                else if (k.startsWith('on') && typeof v === 'function') {
                    el.addEventListener(k.slice(2).toLowerCase(), v);
                } else {
                    el.setAttribute(k, v);
                }
            }
        }
        for (const child of children) {
            if (typeof child === 'string') {
                el.appendChild(document.createTextNode(child));
            } else if (child) {
                el.appendChild(child);
            }
        }
        return el;
    }

    /** HTML-escape a string */
    function esc(s) {
        const d = document.createElement('div');
        d.textContent = s || '';
        return d.innerHTML;
    }

    // =========================================================================
    // Section 3 — API Layer
    // =========================================================================

    async function api(method, path, body) {
        const opts = { method, headers: { 'Content-Type': 'application/json' } };
        if (body) opts.body = JSON.stringify(body);
        const resp = await fetch(path, opts);
        if (!resp.ok) {
            const text = await resp.text();
            throw new Error(`${resp.status}: ${text}`);
        }
        if (resp.status === 204) return null;
        return resp.json();
    }

    async function refreshInstances() {
        try {
            const list = await api('GET', '/api/instances') || [];
            const hash = JSON.stringify(list.map(i => [i.id, i.status, i.working_directory]));
            if (hash === store._instanceHash) return; // no changes
            store._instanceHash = hash;

            store.instances.clear();
            for (const inst of list) {
                store.instances.set(inst.id, inst);
            }
            renderInstances();
        } catch (e) {
            console.error('refreshInstances:', e);
        }
    }

    async function createInstance(workDir) {
        try {
            await api('POST', '/api/instances', { working_directory: workDir });
            store._instanceHash = ''; // force refresh
            await refreshInstances();
        } catch (e) {
            alert('Failed to create instance: ' + e.message);
        }
    }

    async function deleteInstance(id) {
        try {
            await api('DELETE', `/api/instances/${id}`);
        } catch (e) {
            // ignore
        }
        if (store.selectedInstanceId === id) {
            store.selectedInstanceId = null;
            store.selectedSessionId = null;
            store.sessions.clear();
            store.messages.clear();
            store.streamingParts.clear();
            closeEventSource();
            renderSessions();
            renderMessages();
            updateHeader();
        }
        store._instanceHash = '';
        await refreshInstances();
    }

    async function loadSessions(instanceId) {
        try {
            const list = await api('GET', `/api/instances/${instanceId}/sessions`) || [];
            store.sessions.clear();
            for (const s of list) {
                store.sessions.set(s.id, s);
            }
        } catch (e) {
            console.error('loadSessions:', e);
            store.sessions.clear();
        }
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
        } catch (e) {
            alert('Failed to create session: ' + e.message);
        }
    }

    async function loadMessages(sessionId) {
        try {
            const msgs = await api('GET', `/api/sessions/${sessionId}/messages`) || [];
            // Only update if this is still the active session
            if (store.selectedSessionId !== sessionId) return;
            store.messages.clear();
            for (const m of msgs) {
                store.messages.set(m.info.id, m);
            }
            store.streamingParts.clear();
            renderMessages();
        } catch (e) {
            console.error('loadMessages:', e);
        }
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
    // Section 4 — SSE Event Handling
    // =========================================================================

    function closeEventSource() {
        if (store.eventSource) {
            store.eventSource.close();
            store.eventSource = null;
        }
    }

    function connectSSE(sessionId) {
        closeEventSource();
        const es = new EventSource(`/api/sessions/${sessionId}/events`);
        store.eventSource = es;

        es.onmessage = function (e) {
            if (store.selectedSessionId !== sessionId) {
                es.close();
                return;
            }
            try {
                const event = JSON.parse(e.data);
                routeEvent(event);
            } catch (err) {
                // ignore parse errors
            }
        };

        es.onerror = function () {
            // EventSource auto-reconnects
        };
    }

    function routeEvent(event) {
        const type = event.type;
        const props = event.properties || {};

        switch (type) {
            case 'message.part.delta':
                handlePartDelta(props);
                break;
            case 'message.part.updated':
                handlePartUpdated(props);
                break;
            case 'message.updated':
                handleMessageUpdated(props);
                break;
            case 'session.status':
                handleSessionStatus(props);
                break;
            case 'session.updated':
                handleSessionUpdated(props);
                break;
            case 'session.idle':
                handleSessionIdle();
                break;
            default:
                console.debug('Unknown SSE event:', type, props);
        }
    }

    function handlePartDelta(props) {
        const { partID, delta, field, messageID } = props;
        if (!partID || delta === undefined) return;

        if (!store.streamingParts.has(partID)) {
            store.streamingParts.set(partID, { type: 'text', messageID });
        }
        const part = store.streamingParts.get(partID);

        // Accumulate any field, not just text
        if (field === 'text') {
            part.text = (part.text || '') + delta;
        } else if (field === 'toolName') {
            part.toolName = (part.toolName || '') + delta;
        } else if (field === 'args') {
            part.args = (part.args || '') + delta;
        } else if (field === 'result') {
            part.result = (part.result || '') + delta;
        } else if (field === 'reasoning') {
            part.reasoning = (part.reasoning || '') + delta;
        } else if (field) {
            // Generic accumulation for unknown fields
            part[field] = (part[field] || '') + delta;
        }

        renderStreamingArea();
    }

    function handlePartUpdated(props) {
        const part = props.part;
        if (!part) return;
        const id = part.id || part.partID;
        if (!id) return;
        // Store entire part object as-is
        store.streamingParts.set(id, part);
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
        const statusType = props.status?.type;
        if (statusType === 'busy') {
            store.sessionBusy = true;
            store.sessionBusyTool = props.status?.tool || null;
            updateInputState();
            updateHeaderStatus();
        } else if (statusType === 'idle') {
            store.sessionBusy = false;
            store.sessionBusyTool = null;
            updateInputState();
            updateHeaderStatus();
        }
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
        updateInputState();
        updateHeaderStatus();
        // Reload full messages for final clean state
        if (store.selectedSessionId) {
            loadMessages(store.selectedSessionId);
        }
    }

    // =========================================================================
    // Section 5 — Optimistic User Messages & Send
    // =========================================================================

    function doSend() {
        const input = document.getElementById('message-input');
        const content = input.value.trim();
        if (!content || !store.selectedSessionId || store.sessionBusy) return;
        input.value = '';
        input.style.height = 'auto';

        // Add optimistic message
        const optimisticId = '_opt_' + Date.now();
        store.messages.set(optimisticId, {
            info: {
                id: optimisticId,
                role: 'user',
                time: { created: Date.now() / 1000 },
            },
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
    // Section 6 — Rich Message Rendering
    // =========================================================================

    function renderMessages() {
        const inner = document.getElementById('messages-inner');
        if (!inner) return;

        if (!store.messages.size && !store.streamingParts.size) {
            inner.innerHTML = '<div class="flex items-center justify-center text-gray-600 py-20"><p>No messages yet.</p></div>';
            return;
        }

        // Build ordered list from map (messages come ordered from API)
        const msgs = Array.from(store.messages.values());

        // Reconcile message elements
        reconcileList(
            inner,
            msgs,
            (msg) => msg.info.id,
            (msg) => renderMessageElement(msg),
            (el, msg) => updateMessageElement(el, msg)
        );

        renderStreamingArea();
        scrollToBottom();
    }

    function renderMessageElement(msg) {
        const role = msg.info?.role || 'assistant';
        const isUser = role === 'user';
        const isOptimistic = msg._optimistic;

        const wrapper = h('div', {
            className: `flex ${isUser ? 'justify-end' : 'justify-start'}`,
            'data-msg-id': msg.info.id,
        });

        if (isUser) {
            const bubble = h('div', {
                className: `max-w-3xl rounded-lg px-4 py-3 bg-blue-600 ${isOptimistic ? 'opacity-70' : ''}`,
            });
            const textContent = extractText(msg.parts);
            bubble.appendChild(h('pre', {
                className: 'whitespace-pre-wrap text-sm leading-relaxed',
                textContent: textContent,
            }));
            wrapper.appendChild(bubble);
        } else {
            const bubble = h('div', {
                className: 'max-w-3xl rounded-lg px-4 py-3 bg-gray-800 border border-gray-700 w-full',
            });
            const parts = parseParts(msg.parts);
            for (const part of parts) {
                bubble.appendChild(renderMessagePart(part));
            }
            // If no visible content was added, show a subtle placeholder
            if (!parts.length) {
                bubble.appendChild(h('span', { className: 'text-gray-500 text-sm italic', textContent: '(empty)' }));
            }
            wrapper.appendChild(bubble);
        }

        return wrapper;
    }

    function updateMessageElement(el, msg) {
        const role = msg.info?.role || 'assistant';
        const isUser = role === 'user';

        if (isUser) {
            const pre = el.querySelector('pre');
            if (pre) {
                const text = extractText(msg.parts);
                if (pre.textContent !== text) pre.textContent = text;
            }
            // Remove optimistic styling if confirmed
            const bubble = el.querySelector('.bg-blue-600');
            if (bubble && !msg._optimistic) {
                bubble.classList.remove('opacity-70');
            }
        } else {
            // For assistant messages, rebuild parts on update
            const bubble = el.querySelector('.bg-gray-800');
            if (bubble) {
                bubble.innerHTML = '';
                const parts = parseParts(msg.parts);
                for (const part of parts) {
                    bubble.appendChild(renderMessagePart(part));
                }
                if (!parts.length) {
                    bubble.appendChild(h('span', { className: 'text-gray-500 text-sm italic', textContent: '(empty)' }));
                }
            }
        }
    }

    /** Extract text content from parts array (handles both parsed and raw JSON) */
    function extractText(parts) {
        if (!parts || !parts.length) return '';
        const texts = [];
        for (const p of parts) {
            const parsed = typeof p === 'string' ? tryParseJSON(p) : p;
            if (parsed && parsed.type === 'text' && parsed.text) {
                texts.push(parsed.text);
            }
        }
        return texts.join('\n');
    }

    /** Parse parts array — handles both parsed objects and raw JSON strings */
    function parseParts(parts) {
        if (!parts || !parts.length) return [];
        const result = [];
        for (const p of parts) {
            const parsed = typeof p === 'string' ? tryParseJSON(p) : p;
            if (parsed) result.push(parsed);
        }
        return result;
    }

    function tryParseJSON(s) {
        try { return JSON.parse(s); } catch { return null; }
    }

    function renderMessagePart(part) {
        if (!part || !part.type) {
            // Unknown structure — show collapsed JSON
            return renderUnknownPart(part);
        }

        switch (part.type) {
            case 'text':
                return renderTextPart(part);
            case 'tool-invocation':
                return renderToolInvocation(part);
            case 'tool-result':
                return renderToolResult(part);
            case 'step-start':
                return renderStepStart(part);
            case 'step-finish':
                return renderStepFinish(part);
            case 'reasoning':
                return renderReasoningPart(part);
            default:
                return renderUnknownPart(part);
        }
    }

    function renderTextPart(part) {
        const div = h('div', { className: 'prose text-sm' });
        div.innerHTML = renderMarkdown(part.text || '');
        return div;
    }

    function renderToolInvocation(part) {
        const toolName = part.toolName || part.name || 'Tool';
        const state = part.state || part.toolState || 'running';
        const args = part.args;
        const result = part.result;

        const statusDot = state === 'running'
            ? 'bg-yellow-400 animate-pulse'
            : state === 'error'
                ? 'bg-red-400'
                : 'bg-green-400';

        const details = h('details', { className: 'my-2 group' });

        const summary = h('summary', {
            className: 'flex items-center gap-2 cursor-pointer text-sm text-gray-300 hover:text-gray-100 select-none',
        });
        summary.appendChild(h('span', { className: `w-2 h-2 rounded-full ${statusDot} flex-shrink-0` }));
        summary.appendChild(h('span', { className: 'font-mono text-xs', textContent: toolName }));
        if (state === 'running') {
            summary.appendChild(h('span', { className: 'text-gray-500 text-xs', textContent: 'running...' }));
        }
        details.appendChild(summary);

        const content = h('div', { className: 'mt-2 ml-4 space-y-2' });

        if (args) {
            const argsStr = typeof args === 'string' ? args : JSON.stringify(args, null, 2);
            const argsBlock = h('div', { className: 'text-xs' });
            argsBlock.appendChild(h('div', { className: 'text-gray-500 mb-1', textContent: 'Arguments:' }));
            const pre = h('pre', {
                className: 'bg-gray-900 rounded p-2 overflow-x-auto max-h-60 overflow-y-auto text-gray-300 custom-scrollbar',
            });
            pre.appendChild(h('code', { textContent: argsStr }));
            argsBlock.appendChild(pre);
            content.appendChild(argsBlock);
        }

        if (result) {
            const resultStr = typeof result === 'string' ? result : JSON.stringify(result, null, 2);
            const resultBlock = h('div', { className: 'text-xs' });
            resultBlock.appendChild(h('div', { className: 'text-gray-500 mb-1', textContent: 'Result:' }));
            const pre = h('pre', {
                className: 'bg-gray-900 rounded p-2 overflow-x-auto max-h-60 overflow-y-auto text-gray-300 custom-scrollbar',
            });
            pre.appendChild(h('code', { textContent: resultStr }));
            resultBlock.appendChild(pre);
            content.appendChild(resultBlock);
        }

        details.appendChild(content);
        return details;
    }

    function renderToolResult(part) {
        const result = part.result || part.text || part.content || '';
        const resultStr = typeof result === 'string' ? result : JSON.stringify(result, null, 2);

        const details = h('details', { className: 'my-2' });
        const summary = h('summary', {
            className: 'flex items-center gap-2 cursor-pointer text-sm text-gray-400 hover:text-gray-200 select-none',
        });
        summary.appendChild(h('span', { className: 'w-2 h-2 rounded-full bg-gray-500 flex-shrink-0' }));
        summary.appendChild(h('span', { className: 'text-xs', textContent: 'Tool Result' }));
        details.appendChild(summary);

        const pre = h('pre', {
            className: 'mt-2 ml-4 bg-gray-900 rounded p-2 overflow-x-auto max-h-60 overflow-y-auto text-xs text-gray-300 custom-scrollbar',
        });
        pre.appendChild(h('code', { textContent: resultStr }));
        details.appendChild(pre);

        return details;
    }

    function renderStepStart(part) {
        const div = h('div', {
            className: 'flex items-center gap-2 my-2 text-xs text-gray-500',
        });
        div.appendChild(h('div', { className: 'flex-1 border-t border-gray-800' }));
        div.appendChild(h('span', { className: 'animate-pulse', textContent: 'Working...' }));
        div.appendChild(h('div', { className: 'flex-1 border-t border-gray-800' }));
        return div;
    }

    function renderStepFinish(part) {
        // Subtle done marker
        const div = h('div', {
            className: 'flex items-center gap-2 my-1 text-xs text-gray-600',
        });
        div.appendChild(h('div', { className: 'flex-1 border-t border-gray-800/50' }));
        return div;
    }

    function renderReasoningPart(part) {
        const text = part.text || part.reasoning || '';
        if (!text) return h('span');

        const details = h('details', { className: 'my-2' });
        const summary = h('summary', {
            className: 'cursor-pointer text-sm text-gray-500 hover:text-gray-300 italic select-none',
            textContent: 'Thinking...',
        });
        details.appendChild(summary);

        const content = h('div', {
            className: 'mt-1 ml-4 text-sm text-gray-400 italic',
        });
        content.textContent = text;
        details.appendChild(content);

        return details;
    }

    function renderUnknownPart(part) {
        const details = h('details', { className: 'my-1' });
        const typeName = part?.type || 'unknown';
        const summary = h('summary', {
            className: 'cursor-pointer text-xs text-gray-600 hover:text-gray-400 select-none',
            textContent: `[${typeName}]`,
        });
        details.appendChild(summary);

        const pre = h('pre', {
            className: 'mt-1 ml-4 bg-gray-900 rounded p-2 overflow-x-auto max-h-40 overflow-y-auto text-xs text-gray-400 custom-scrollbar',
        });
        pre.appendChild(h('code', { textContent: JSON.stringify(part, null, 2) }));
        details.appendChild(pre);

        return details;
    }

    // =========================================================================
    // Section 7 — Streaming Area
    // =========================================================================

    function renderStreamingArea() {
        const inner = document.getElementById('messages-inner');
        if (!inner) return;

        let streamEl = document.getElementById('streaming-area');

        if (!store.streamingParts.size) {
            if (streamEl) streamEl.remove();
            return;
        }

        if (!streamEl) {
            streamEl = h('div', { id: 'streaming-area', className: 'flex justify-start' });
            inner.appendChild(streamEl);
        }

        const bubble = h('div', {
            className: 'max-w-3xl rounded-lg px-4 py-3 bg-gray-800 border border-gray-700 w-full space-y-2',
        });

        // Separate tool invocations from text
        const toolParts = [];
        const textParts = [];
        const otherParts = [];

        for (const [id, part] of store.streamingParts) {
            if (part.type === 'tool-invocation' || part.toolName) {
                toolParts.push(part);
            } else if (part.type === 'text' || part.text) {
                textParts.push(part);
            } else if (part.type === 'reasoning' || part.reasoning) {
                otherParts.push(part);
            }
        }

        // Render reasoning first
        for (const part of otherParts) {
            const text = part.reasoning || part.text || '';
            if (text) {
                const details = h('details', { className: 'my-1' });
                details.setAttribute('open', '');
                details.appendChild(h('summary', {
                    className: 'cursor-pointer text-sm text-gray-500 italic select-none',
                    textContent: 'Thinking...',
                }));
                details.appendChild(h('div', {
                    className: 'mt-1 text-sm text-gray-400 italic',
                    textContent: text,
                }));
                bubble.appendChild(details);
            }
        }

        // Render tool invocations
        for (const part of toolParts) {
            bubble.appendChild(renderToolInvocation({
                type: 'tool-invocation',
                toolName: part.toolName || part.name || 'Tool',
                state: part.state || 'running',
                args: part.args,
                result: part.result,
            }));
        }

        // Render streaming text with cursor
        const allText = textParts.map(p => p.text || '').join('');
        if (allText) {
            const textDiv = h('div', { className: 'prose text-sm' });
            textDiv.innerHTML = renderMarkdown(allText);

            // Add blinking cursor at end
            const cursor = h('span', {
                className: 'inline-block w-1.5 h-4 bg-blue-400 animate-pulse ml-0.5 align-text-bottom',
            });
            textDiv.appendChild(cursor);
            bubble.appendChild(textDiv);
        } else if (!toolParts.length && !otherParts.length) {
            // Show a loading indicator if nothing to show yet
            bubble.appendChild(h('span', {
                className: 'inline-block w-1.5 h-4 bg-blue-400 animate-pulse',
            }));
        }

        streamEl.innerHTML = '';
        streamEl.appendChild(bubble);
        scrollToBottom();
    }

    // =========================================================================
    // Section 8 — Sidebar Renderers
    // =========================================================================

    function renderInstances() {
        const list = document.getElementById('instance-list');
        if (!list) return;

        const items = Array.from(store.instances.values());

        if (!items.length) {
            list.innerHTML = '<p class="text-xs text-gray-500 py-2">No instances running</p>';
            return;
        }

        reconcileList(
            list,
            items,
            (inst) => inst.id,
            (inst) => createInstanceElement(inst),
            (el, inst) => updateInstanceElement(el, inst)
        );
    }

    function createInstanceElement(inst) {
        const selected = inst.id === store.selectedInstanceId;
        const div = h('div', {
            className: `flex items-center gap-2 px-2 py-1.5 rounded cursor-pointer group ${selected ? 'bg-gray-700' : 'hover:bg-gray-800'}`,
            'data-action': 'select-instance',
            'data-id': inst.id,
        });

        const color = statusColor(inst.status);
        div.appendChild(h('span', { className: `w-2 h-2 rounded-full ${color} flex-shrink-0` }));

        const dir = inst.working_directory.split('/').pop() || inst.working_directory;
        div.appendChild(h('span', {
            className: 'text-sm truncate flex-1',
            textContent: dir,
            title: inst.working_directory,
        }));

        if (inst.port) {
            div.appendChild(h('span', {
                className: 'text-xs text-gray-500 flex-shrink-0',
                textContent: ':' + inst.port,
            }));
        }

        div.appendChild(h('button', {
            className: 'text-gray-500 hover:text-red-400 text-xs px-1 opacity-0 group-hover:opacity-100 transition-opacity flex-shrink-0',
            'data-action': 'delete-instance',
            'data-id': inst.id,
            textContent: '\u00d7',
        }));

        return div;
    }

    function updateInstanceElement(el, inst) {
        const selected = inst.id === store.selectedInstanceId;
        el.className = `flex items-center gap-2 px-2 py-1.5 rounded cursor-pointer group ${selected ? 'bg-gray-700' : 'hover:bg-gray-800'}`;

        const dot = el.querySelector('span:first-child');
        if (dot) {
            const color = statusColor(inst.status);
            dot.className = `w-2 h-2 rounded-full ${color} flex-shrink-0`;
        }
    }

    function statusColor(status) {
        const colors = {
            running: 'bg-green-500',
            starting: 'bg-yellow-500 animate-pulse',
            error: 'bg-red-500',
            stopped: 'bg-gray-500',
        };
        return colors[status] || 'bg-gray-500';
    }

    function renderSessions() {
        const list = document.getElementById('session-list');
        if (!list) return;

        const items = Array.from(store.sessions.values());

        if (!items.length) {
            list.innerHTML = '<p class="text-xs text-gray-500 py-2">No sessions</p>';
            return;
        }

        reconcileList(
            list,
            items,
            (sess) => sess.id,
            (sess) => createSessionElement(sess),
            (el, sess) => updateSessionElement(el, sess)
        );
    }

    function createSessionElement(sess) {
        const selected = sess.id === store.selectedSessionId;
        const title = sess.title || 'Untitled';

        const div = h('div', {
            className: `flex items-center gap-2 px-2 py-1.5 rounded cursor-pointer text-sm ${selected ? 'bg-gray-700 text-white' : 'text-gray-400 hover:bg-gray-800'}`,
            'data-action': 'select-session',
            'data-id': sess.id,
            title: title,
        });

        // Busy/idle dot
        const isBusy = selected && store.sessionBusy;
        div.appendChild(h('span', {
            className: `w-1.5 h-1.5 rounded-full flex-shrink-0 ${isBusy ? 'bg-yellow-400 animate-pulse' : 'bg-gray-600'}`,
        }));

        div.appendChild(h('span', {
            className: 'truncate',
            textContent: title,
        }));

        return div;
    }

    function updateSessionElement(el, sess) {
        const selected = sess.id === store.selectedSessionId;
        const title = sess.title || 'Untitled';

        el.className = `flex items-center gap-2 px-2 py-1.5 rounded cursor-pointer text-sm ${selected ? 'bg-gray-700 text-white' : 'text-gray-400 hover:bg-gray-800'}`;
        el.title = title;

        const dot = el.querySelector('span:first-child');
        if (dot) {
            const isBusy = selected && store.sessionBusy;
            dot.className = `w-1.5 h-1.5 rounded-full flex-shrink-0 ${isBusy ? 'bg-yellow-400 animate-pulse' : 'bg-gray-600'}`;
        }

        const titleEl = el.querySelector('span:last-child');
        if (titleEl && titleEl.textContent !== title) {
            titleEl.textContent = title;
        }
    }

    // =========================================================================
    // Section 9 — Sidebar Resize
    // =========================================================================

    function initResize() {
        const handle = document.getElementById('resize-handle');
        const sidebar = document.getElementById('sidebar');
        if (!handle || !sidebar) return;

        let startX, startWidth;

        handle.addEventListener('mousedown', (e) => {
            e.preventDefault();
            startX = e.clientX;
            startWidth = sidebar.offsetWidth;
            document.body.style.cursor = 'col-resize';
            document.body.style.userSelect = 'none';

            function onMouseMove(e) {
                const diff = e.clientX - startX;
                const newWidth = Math.min(480, Math.max(224, startWidth + diff));
                sidebar.style.width = newWidth + 'px';
            }

            function onMouseUp() {
                document.removeEventListener('mousemove', onMouseMove);
                document.removeEventListener('mouseup', onMouseUp);
                document.body.style.cursor = '';
                document.body.style.userSelect = '';
            }

            document.addEventListener('mousemove', onMouseMove);
            document.addEventListener('mouseup', onMouseUp);
        });
    }

    // =========================================================================
    // Section 10 — Selection, Input & Event Delegation
    // =========================================================================

    function selectInstance(id) {
        store.selectedInstanceId = id;
        store.selectedSessionId = null;
        store.sessions.clear();
        store.messages.clear();
        store.streamingParts.clear();
        store.sessionBusy = false;
        store.sessionBusyTool = null;
        closeEventSource();
        renderInstances();
        renderSessions();
        renderMessages();
        updateHeader();
        updateHeaderStatus();
        document.getElementById('btn-new-session').classList.remove('hidden');
        document.getElementById('input-area').classList.add('hidden');
        loadSessions(id);
    }

    function selectSession(id) {
        if (store.selectedSessionId === id) return;
        store.selectedSessionId = id;
        store.messages.clear();
        store.streamingParts.clear();
        store.sessionBusy = false;
        store.sessionBusyTool = null;
        renderSessions();
        renderMessages();
        updateHeader();
        updateHeaderStatus();
        updateInputState();
        document.getElementById('input-area').classList.remove('hidden');
        loadMessages(id);
        connectSSE(id);
    }

    function updateHeader() {
        const el = document.getElementById('main-header');
        if (!el) return;
        if (store.selectedSessionId) {
            const sess = store.sessions.get(store.selectedSessionId);
            el.textContent = sess?.title || 'Session';
        } else if (store.selectedInstanceId) {
            const inst = store.instances.get(store.selectedInstanceId);
            el.textContent = inst ? `Instance: ${inst.working_directory}` : 'Instance';
        } else {
            el.textContent = 'Select an instance to get started';
        }
    }

    function updateHeaderStatus() {
        const el = document.getElementById('header-status');
        if (!el) return;
        if (!store.selectedSessionId) {
            el.innerHTML = '';
            return;
        }
        if (store.sessionBusy) {
            const toolText = store.sessionBusyTool ? ` · ${esc(store.sessionBusyTool)}` : '';
            el.innerHTML = `<span class="w-2 h-2 rounded-full bg-yellow-400 animate-pulse inline-block"></span><span>Busy${toolText}</span>`;
        } else {
            el.innerHTML = '<span class="w-2 h-2 rounded-full bg-green-500 inline-block"></span><span>Idle</span>';
        }
    }

    function updateInputState() {
        const btn = document.getElementById('btn-send');
        const input = document.getElementById('message-input');
        if (!btn || !input) return;
        btn.disabled = store.sessionBusy;
        btn.textContent = store.sessionBusy ? 'Working...' : 'Send';
        input.disabled = store.sessionBusy;
        if (!store.sessionBusy) input.focus();
    }

    function scrollToBottom() {
        const el = document.getElementById('messages');
        if (el) {
            requestAnimationFrame(() => {
                el.scrollTop = el.scrollHeight;
            });
        }
    }

    // Event delegation
    document.addEventListener('click', function (e) {
        const target = e.target.closest('[data-action]');
        if (!target) return;
        const action = target.dataset.action;
        const id = target.dataset.id;

        switch (action) {
            case 'select-instance':
                selectInstance(id);
                break;
            case 'delete-instance':
                e.stopPropagation();
                if (confirm('Stop this instance?')) deleteInstance(id);
                break;
            case 'select-session':
                selectSession(id);
                break;
        }
    });

    // New instance button
    document.getElementById('btn-new-instance').addEventListener('click', () => {
        document.getElementById('modal-new-instance').classList.remove('hidden');
        document.getElementById('input-workdir').value = '';
        document.getElementById('input-workdir').focus();
    });

    // Cancel instance creation
    document.getElementById('btn-cancel-instance').addEventListener('click', () => {
        document.getElementById('modal-new-instance').classList.add('hidden');
    });

    // Create instance
    document.getElementById('btn-create-instance').addEventListener('click', () => {
        const workDir = document.getElementById('input-workdir').value.trim();
        if (!workDir) return;
        document.getElementById('modal-new-instance').classList.add('hidden');
        createInstance(workDir);
    });

    // Enter to submit in workdir input
    document.getElementById('input-workdir').addEventListener('keydown', (e) => {
        if (e.key === 'Enter') {
            e.preventDefault();
            document.getElementById('btn-create-instance').click();
        }
    });

    // New session button
    document.getElementById('btn-new-session').addEventListener('click', () => {
        if (store.selectedInstanceId) createSession(store.selectedInstanceId);
    });

    // Send on Enter, newline on Shift+Enter
    document.getElementById('message-input').addEventListener('keydown', (e) => {
        if (e.key === 'Enter' && !e.shiftKey) {
            e.preventDefault();
            doSend();
        }
    });

    document.getElementById('btn-send').addEventListener('click', (e) => {
        e.preventDefault();
        doSend();
    });

    // Auto-resize textarea
    document.getElementById('message-input').addEventListener('input', function () {
        this.style.height = 'auto';
        this.style.height = Math.min(this.scrollHeight, 200) + 'px';
    });

    // Close modal on backdrop click
    document.getElementById('modal-new-instance').addEventListener('click', (e) => {
        if (e.target === e.currentTarget) {
            e.currentTarget.classList.add('hidden');
        }
    });

    // Escape to close modal
    document.addEventListener('keydown', (e) => {
        if (e.key === 'Escape') {
            document.getElementById('modal-new-instance').classList.add('hidden');
        }
    });

    // =========================================================================
    // Section 11 — Markdown Configuration
    // =========================================================================

    let markdownReady = false;

    function initMarkdown() {
        if (typeof marked === 'undefined') {
            // Retry after scripts load
            setTimeout(initMarkdown, 100);
            return;
        }

        const renderer = new marked.Renderer();

        renderer.code = function ({ text, lang }) {
            if (typeof hljs !== 'undefined' && lang && hljs.getLanguage(lang)) {
                try {
                    const highlighted = hljs.highlight(text, { language: lang }).value;
                    return `<pre class="hljs rounded-lg overflow-x-auto"><code class="language-${esc(lang)}">${highlighted}</code></pre>`;
                } catch (e) {
                    // fall through
                }
            }
            return `<pre class="hljs rounded-lg overflow-x-auto"><code>${esc(text)}</code></pre>`;
        };

        renderer.codespan = function ({ text }) {
            return `<code class="bg-gray-800 text-gray-200 px-1.5 py-0.5 rounded text-sm">${esc(text)}</code>`;
        };

        marked.setOptions({
            renderer: renderer,
            gfm: true,
            breaks: true,
        });

        markdownReady = true;
    }

    function renderMarkdown(text) {
        if (!text) return '';
        if (markdownReady && typeof marked !== 'undefined') {
            try {
                return marked.parse(text);
            } catch (e) {
                console.error('Markdown parse error:', e);
            }
        }
        // Fallback: escaped text with newlines
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
