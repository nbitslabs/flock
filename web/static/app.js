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

        flockAgentActive: false,
        flockAgentId: null,
        flockAgentSessionId: null,
        viewingFlockAgent: false,
        selectedInstanceId: null,
        selectedSessionId: null,
        sessionBusy: false,
        sessionBusyTool: null,
        sessionQuestion: false,
        _questionToolName: null,
        eventSource: null,
        _instanceHash: '',
        _lastSentText: null,  // track user message to filter streaming echoes
        _sessionHash: '',
        _darkMode: true,
        _flockAgentHash: '',
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
    // Section 3 — Theme
    // =========================================================================

    function isDarkMode() { return store._darkMode; }

    function toggleTheme() {
        store._darkMode = !store._darkMode;
        const html = document.documentElement;
        if (store._darkMode) {
            html.classList.add('dark');
        } else {
            html.classList.remove('dark');
        }
        localStorage.setItem('theme', store._darkMode ? 'dark' : 'light');
        renderMessages();
        renderStreamingArea();
        renderInstances();
        renderSessions();
    }

    function initTheme() {
        const saved = localStorage.getItem('theme');
        if (saved) {
            store._darkMode = saved === 'dark';
        } else {
            store._darkMode = window.matchMedia('(prefers-color-scheme: dark)').matches;
        }
        const html = document.documentElement;
        if (store._darkMode) {
            html.classList.add('dark');
        } else {
            html.classList.remove('dark');
        }
    }

    function getTextColorClass(light, dark) {
        return isDarkMode() ? dark : light;
    }

    function getBgColorClass(light, dark) {
        return isDarkMode() ? dark : light;
    }

    function getBorderColorClass(light, dark) {
        return isDarkMode() ? dark : light;
    }

    // =========================================================================
    // Section 3 — URL State
    // =========================================================================

    function updateURL() {
        if (store.selectedSessionId) {
            window.location.hash = `i/${encodeURIComponent(store.selectedInstanceId)}/s/${encodeURIComponent(store.selectedSessionId)}`;
        } else if (store.selectedInstanceId) {
            window.location.hash = `i/${encodeURIComponent(store.selectedInstanceId)}`;
        } else {
            window.location.hash = '';
        }
    }

    function parseURL() {
        const hash = window.location.hash.slice(1);
        if (!hash) return { instanceId: null, sessionId: null };
        
        const parts = hash.split('/');
        if (parts[0] === 'i' && parts[1]) {
            const instanceId = decodeURIComponent(parts[1]);
            if (parts[2] === 's' && parts[3]) {
                const sessionId = decodeURIComponent(parts[3]);
                return { instanceId, sessionId };
            }
            return { instanceId, sessionId: null };
        }
        return { instanceId: null, sessionId: null };
    }

    async function restoreFromURL() {
        const { instanceId, sessionId } = parseURL();
        if (!instanceId) return;

        await refreshInstances();
        if (!store.instances.has(instanceId)) return;

        store.selectedInstanceId = instanceId;
        renderInstances();
        updateHeader();
        document.getElementById('btn-new-session').classList.remove('hidden');
        document.getElementById('input-area').classList.add('hidden');

        await loadSessions(instanceId);

        if (sessionId && store.sessions.has(sessionId)) {
            await selectSession(sessionId);
        }
    }

    // =========================================================================
    // Section 4 — API
    // =========================================================================

    async function api(method, path, body) {
        const opts = { method, headers: { 'Content-Type': 'application/json' }, credentials: 'same-origin' };
        if (body) opts.body = JSON.stringify(body);
        const resp = await fetch(path, opts);
        if (resp.status === 401) {
            window.location.href = '/login';
            return;
        }
        if (!resp.ok) throw new Error(`${resp.status}: ${await resp.text()}`);
        if (resp.status === 204) return null;
        return resp.json();
    }

    async function refreshInstances() {
        try {
            const list = await api('GET', '/api/instances') || [];
            const hash = JSON.stringify(list.map(i => [i.id, i.status, i.working_directory, i.last_heartbeat_at, i.org, i.repo]));
            if (hash === store._instanceHash) return;
            store._instanceHash = hash;
            store.instances.clear();
            for (const inst of list) store.instances.set(inst.id, inst);
            renderInstances();
        } catch (e) { console.error('refreshInstances:', e); }
    }

    async function createInstance(githubURL) {
        try {
            await api('POST', '/api/instances', { github_url: githubURL });
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
            updateURL();
        }
        store._instanceHash = '';
        await refreshInstances();
    }

    async function loadSessions(instanceId) {
        try {
            const list = await api('GET', `/api/instances/${instanceId}/sessions`) || [];
            store.sessions.clear();
            store._sessionHash = '';
            for (const s of list) store.sessions.set(s.id, s);
        } catch (e) { console.error('loadSessions:', e); store.sessions.clear(); }
        renderSessions();
    }

    async function refreshSessions() {
        if (!store.selectedInstanceId) return;
        try {
            const list = await api('GET', `/api/instances/${store.selectedInstanceId}/sessions`) || [];
            const hash = JSON.stringify(list.map(s => [s.id, s.title]));
            if (hash === store._sessionHash) return;
            store._sessionHash = hash;
            store.sessions.clear();
            for (const s of list) store.sessions.set(s.id, s);
            renderSessions();
        } catch (e) { console.error('refreshSessions:', e); }
    }

    async function createSession(instanceId) {
        try {
            const session = await api('POST', `/api/instances/${instanceId}/sessions`);
            await loadSessions(instanceId);
            selectSession(session.id);
        } catch (e) { alert('Failed to create session: ' + e.message); }
    }

    async function deleteSession(id) {
        try { await api('DELETE', `/api/sessions/${id}`); } catch (e) { /* ignore */ }
        if (store.selectedSessionId === id) {
            store.selectedSessionId = null;
            store.messages.clear();
            store.streamingParts.clear();
            closeEventSource();
            renderSessions(); renderMessages(); updateHeader();
            updateURL();
            document.getElementById('input-area').classList.add('hidden');
        }
        store._sessionHash = '';
        if (store.selectedInstanceId) await loadSessions(store.selectedInstanceId);
    }

    // Load messages from API.
    // merge=false (default): clear store first — used on session select and session idle.
    // merge=true: keep existing messages and layer API data on top (currently unused).
    async function loadMessages(sessionId, merge) {
        try {
            const msgs = await api('GET', `/api/sessions/${sessionId}/messages`) || [];
            if (store.selectedSessionId !== sessionId) return;

            if (!merge) {
                store.messages.clear();
            }

            // Add/update messages from API (overwrites stale versions)
            for (const m of msgs) {
                if (!m.info?.id) continue;
                store.messages.set(m.info.id, m);
            }

            // Clean up optimistic messages only when the API has a renderable replacement
            for (const [id, m] of store.messages) {
                if (!m._optimistic) continue;
                const optText = extractText(m.parts).trim();
                if (!optText) { store.messages.delete(id); continue; }
                for (const apiMsg of msgs) {
                    if (apiMsg.info?.role === 'user' && apiMsg.info?.id &&
                        extractText(apiMsg.parts).trim() === optText) {
                        // Only remove if the API version is in the store and renderable
                        const real = store.messages.get(apiMsg.info.id);
                        if (real && extractText(real.parts).trim()) {
                            store.messages.delete(id);
                        }
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

    // Flock Agent API
    async function refreshFlockAgent() {
        try {
            const data = await api('GET', '/api/flock-agent') || {};
            const hash = JSON.stringify([data.active, data.id, data.status]);
            if (hash === store._flockAgentHash) return;
            store._flockAgentHash = hash;
            store.flockAgentActive = data.active;
            store.flockAgentId = data.id;
            store.flockAgentSessionId = data.session_id;
            renderFlockAgent();
        } catch (e) { console.error('refreshFlockAgent:', e); }
    }

    async function createFlockAgent() {
        try {
            await api('POST', '/api/flock-agent');
            store._flockAgentHash = '';
            await refreshFlockAgent();
        } catch (e) { alert('Failed to create flock agent: ' + e.message); }
    }

    async function rotateFlockAgent() {
        try {
            await api('PUT', '/api/flock-agent/rotate');
            store._flockAgentHash = '';
            await refreshFlockAgent();
        } catch (e) { alert('Failed to rotate flock agent: ' + e.message); }
    }

    async function loadFlockAgentMessages() {
        try {
            const msgs = await api('GET', '/api/flock-agent/messages') || [];
            store.messages.clear();
            for (const m of msgs) {
                if (!m.info?.id) continue;
                store.messages.set(m.info.id, m);
            }
            store.streamingParts.clear();
            store._lastSentText = null;
            renderMessages();
        } catch (e) { console.error('loadFlockAgentMessages:', e); }
    }

    async function sendFlockAgentMessage(content) {
        try {
            await api('POST', '/api/flock-agent/messages', { content });
        } catch (e) {
            console.error('sendFlockAgentMessage:', e);
            alert('Failed to send message: ' + e.message);
            store.sessionBusy = false;
            updateInputState();
        }
    }

    function connectFlockAgentSSE() {
        closeEventSource();
        const es = new EventSource('/api/flock-agent/events');
        store.eventSource = es;
        es.onmessage = function (e) {
            try { routeEvent(JSON.parse(e.data)); } catch (err) { /* ignore */ }
        };
        es.onerror = function () { /* auto-reconnects */ };
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
        if (field === 'toolName' && delta === 'question') {
            store.sessionQuestion = true;
            store._questionToolName = partID;
            renderSessions();
        }
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
        const messageID = part.messageID;
        const existing = store.streamingParts.get(id);
        store.streamingParts.set(id, { ...part, _messageID: messageID || (existing && existing._messageID) });

        const toolName = part.toolName || part.tool || part.name;
        if (toolName === 'question') {
            store.sessionQuestion = true;
            store._questionToolName = id;
            renderSessions();
        }

        // Also attach the part to the settled message so it survives
        // when streaming parts are cleared (especially important for user messages
        // whose message.updated events carry only info, no parts).
        if (messageID) {
            const msg = store.messages.get(messageID);
            if (msg) {
                if (!msg.parts) msg.parts = [];
                const idx = msg.parts.findIndex(p => (p.id || p.partID) === id);
                if (idx >= 0) msg.parts[idx] = part;
                else msg.parts.push(part);
            }
        }

        renderStreamingArea();
    }

    function handleMessageUpdated(props) {
        const msg = props.message || props;
        if (msg && msg.info && msg.info.id) {
            // message.updated events often carry only `info` (no parts).
            // Merge into the existing entry so we never clobber parts with nothing.
            const existing = store.messages.get(msg.info.id);
            if (existing) {
                existing.info = msg.info;
                // Only overwrite parts if the event actually carries them
                if (msg.parts) existing.parts = msg.parts;
            } else {
                store.messages.set(msg.info.id, msg);
            }

            // Remove streaming parts that belong to this settled message
            for (const [partId, part] of store.streamingParts) {
                if (part._messageID === msg.info.id) {
                    store.streamingParts.delete(partId);
                }
            }

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
        store.sessionQuestion = false;
        store._questionToolName = null;
        updateInputState(); updateHeaderStatus();
        // Full reload from API — session.idle means processing is complete,
        // so the API has all committed messages.
        if (store.viewingFlockAgent) loadFlockAgentMessages();
        else if (store.selectedSessionId) loadMessages(store.selectedSessionId, false);
    }

    // =========================================================================
    // Section 5 — Send (with optimistic user message)
    // =========================================================================

    function doSend() {
        const input = document.getElementById('message-input');
        const content = input.value.trim();
        if (!content || (store.sessionBusy && !store.sessionQuestion)) return;
        if (store.viewingFlockAgent) {
            input.value = '';
            input.style.height = 'auto';
            store._lastSentText = content;
            const optId = '_opt_' + Date.now();
            store.messages.set(optId, {
                info: { id: optId, role: 'user', time: { created: Date.now() } },
                parts: [{ type: 'text', text: content }],
                _optimistic: true,
            });
            renderMessages();
            scrollToBottom();
            store.sessionBusy = true;
            if (store.sessionQuestion) {
                store.sessionQuestion = false;
                store._questionToolName = null;
                renderSessions();
                updateHeaderStatus();
            }
            updateInputState();
            sendFlockAgentMessage(content);
            return;
        }
        if (!store.selectedSessionId) return;
        input.value = '';
        input.style.height = 'auto';

        store._lastSentText = content;

        const optId = '_opt_' + Date.now();
        store.messages.set(optId, {
            info: { id: optId, role: 'user', time: { created: Date.now() } },
            parts: [{ type: 'text', text: content }],
            _optimistic: true,
        });
        renderMessages();
        scrollToBottom();

        store.sessionBusy = true;
        if (store.sessionQuestion) {
            store.sessionQuestion = false;
            store._questionToolName = null;
            renderSessions();
            updateHeaderStatus();
        }
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
            // Accept type 'text' or missing type if text/content exists
            if (p.type && p.type !== 'text') continue;
            if (p.text || p.content) texts.push(p.text || p.content);
        }
        return texts.join('\n');
    }

    /** Normalize timestamp to milliseconds (handles both seconds, milliseconds, and ISO date strings) */
    function normalizeTs(t) {
        if (!t) return 0;
        // Handle ISO date strings from SQLite (stored as UTC)
        if (typeof t === 'string') {
            const iso = t.includes('Z') ? t : t + 'Z';
            const d = new Date(iso);
            if (!isNaN(d.getTime())) return d.getTime();
            return 0;
        }
        const n = Number(t);
        if (!n) return 0;
        // If < 1e12 it's seconds; otherwise milliseconds
        return n < 1e12 ? n * 1000 : n;
    }

    function formatTime(ts) {
        if (!ts) return '';
        const ms = normalizeTs(ts);
        if (!ms) return '';
        const d = new Date(ms);
        const now = new Date();
        const time = d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
        // Show date if not today
        if (d.toDateString() !== now.toDateString()) {
            return d.toLocaleDateString([], { month: 'short', day: 'numeric' }) + ' ' + time;
        }
        return time;
    }

    function formatRelativeTime(ts) {
        if (!ts) return '';
        const ms = normalizeTs(ts);
        if (!ms) return '';
        const now = Date.now();
        const diff = now - ms;
        const seconds = Math.floor(diff / 1000);
        if (seconds < 60) return 'just now';
        const minutes = Math.floor(seconds / 60);
        if (minutes < 60) return `${minutes}m ago`;
        const hours = Math.floor(minutes / 60);
        if (hours < 24) return `${hours}h ago`;
        const days = Math.floor(hours / 24);
        return `${days}d ago`;
    }

    function renderMessages() {
        const settled = document.getElementById('settled-messages');
        if (!settled) return;

        const allMsgs = Array.from(store.messages.values());

        // Build set of confirmed (non-optimistic) user message texts
        const confirmedUserTexts = new Set();
        for (const m of allMsgs) {
            if (m._optimistic) continue;
            if (m.info?.role === 'user') {
                const t = extractText(m.parts).trim();
                if (t) confirmedUserTexts.add(t);
            }
        }

        // Hide optimistic messages only when a confirmed renderable version exists
        const msgs = allMsgs.filter(m => {
            if (!m._optimistic) return true;
            return !confirmedUserTexts.has(extractText(m.parts).trim());
        });

        // Sort chronologically (normalize seconds vs milliseconds)
        msgs.sort((a, b) => {
            const ta = normalizeTs(a.info?.time?.created);
            const tb = normalizeTs(b.info?.time?.created);
            return ta - tb;
        });

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
            settled.innerHTML = '<div class="flex items-center justify-center text-gray-500 dark:text-gray-600 py-20"><p>No messages yet.</p></div>';
        } else {
            settled.appendChild(frag);
        }

        renderStreamingArea();
        scrollToBottom();
    }

    function buildUserBubble(msg) {
        const text = extractText(msg.parts);
        if (!text) return null;
        const wrapper = h('div', { className: 'flex flex-col items-end gap-1' });
        wrapper.appendChild(h('div', {
            className: `max-w-3xl rounded-lg px-4 py-3 bg-blue-600 ${msg._optimistic ? 'opacity-60' : ''}`,
        }, h('pre', { className: 'whitespace-pre-wrap text-sm leading-relaxed', textContent: text })));
        const ts = formatTime(msg.info?.time?.created);
        if (ts) wrapper.appendChild(h('span', { className: 'text-xs text-gray-500 dark:text-gray-600 px-1', textContent: ts }));
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

        // Use the latest message's timestamp
        const lastMsg = messages[messages.length - 1];
        const ts = formatTime(lastMsg?.info?.time?.created || lastMsg?.info?.time?.completed);

        const wrapper = h('div', { className: 'flex flex-col items-start gap-1' });
        const bubble = h('div', {
            className: 'max-w-3xl rounded-lg px-4 py-3 bg-gray-100 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 space-y-2',
        });
        for (const el of rendered) bubble.appendChild(el);
        wrapper.appendChild(bubble);
        if (ts) wrapper.appendChild(h('span', { className: 'text-xs text-gray-500 dark:text-gray-600 px-1', textContent: ts }));
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
            className: 'flex items-center gap-2 cursor-pointer text-sm text-gray-600 dark:text-gray-300 hover:text-gray-800 dark:hover:text-gray-100 select-none list-none py-0.5',
        });
        summary.appendChild(h('span', { className: `w-2 h-2 rounded-full ${dotClass} flex-shrink-0` }));
        summary.appendChild(h('code', { className: 'text-xs font-mono text-gray-600 dark:text-gray-300', textContent: toolName }));
        if (title) {
            summary.appendChild(h('span', { className: 'text-gray-400 dark:text-gray-500', textContent: '—' }));
            summary.appendChild(h('span', { className: 'text-xs text-gray-500 dark:text-gray-400 truncate', textContent: title }));
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
                const pre = h('pre', { className: 'bg-gray-100 dark:bg-gray-900 rounded p-2 overflow-x-auto max-h-40 overflow-y-auto text-gray-600 dark:text-gray-400 custom-scrollbar' });
                pre.appendChild(h('code', { textContent: inputStr }));
                body.appendChild(pre);
            }
        }

        if (output) {
            const outputStr = typeof output === 'string' ? output : JSON.stringify(output, null, 2);
            if (outputStr) {
                body.appendChild(h('div', { className: 'text-gray-400 dark:text-gray-500', textContent: 'Output:' }));
                const pre = h('pre', { className: 'bg-gray-100 dark:bg-gray-900 rounded p-2 overflow-x-auto max-h-48 overflow-y-auto text-gray-600 dark:text-gray-400 custom-scrollbar' });
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
            className: 'flex items-center gap-2 cursor-pointer text-sm text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200 select-none list-none py-0.5',
        });
        summary.appendChild(h('span', { className: 'w-2 h-2 rounded-full bg-gray-500 flex-shrink-0' }));
        summary.appendChild(h('span', { className: 'text-xs', textContent: 'Tool Result' }));
        details.appendChild(summary);
        const pre = h('pre', { className: 'mt-1.5 ml-4 bg-gray-100 dark:bg-gray-900 rounded p-2 overflow-x-auto max-h-48 overflow-y-auto text-xs text-gray-600 dark:text-gray-400 custom-scrollbar' });
        pre.appendChild(h('code', { textContent: str }));
        details.appendChild(pre);
        return details;
    }

    function renderReasoningPart(part) {
        const text = part.reasoning || part.text || '';
        if (!text.trim()) return null;
        const details = h('details', { className: 'my-1' });
        details.appendChild(h('summary', {
            className: 'cursor-pointer text-sm text-gray-400 dark:text-gray-500 hover:text-gray-600 dark:hover:text-gray-300 italic select-none list-none py-0.5',
            textContent: 'Thinking…',
        }));
        details.appendChild(h('div', {
            className: 'mt-1 ml-4 text-sm text-gray-500 dark:text-gray-400 italic whitespace-pre-wrap',
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
            className: 'cursor-pointer text-xs text-gray-500 dark:text-gray-600 hover:text-gray-700 dark:hover:text-gray-400 select-none list-none',
            textContent: `[${part.type || 'unknown'}]`,
        }));
        const pre = h('pre', { className: 'mt-1 ml-4 bg-gray-100 dark:bg-gray-900 rounded p-2 overflow-x-auto max-h-32 overflow-y-auto text-xs text-gray-500 dark:text-gray-500 custom-scrollbar' });
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
            className: 'max-w-3xl rounded-lg px-4 py-3 bg-gray-100 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 border-dashed space-y-2',
        });

        // Reasoning
        for (const part of reasoning) {
            const text = part.reasoning || part.text || '';
            if (!text) continue;
            const d = h('details', { className: 'my-1' });
            d.setAttribute('open', '');
            d.appendChild(h('summary', {
                className: 'cursor-pointer text-sm text-gray-400 dark:text-gray-500 italic select-none list-none',
                textContent: 'Thinking…',
            }));
            d.appendChild(h('div', { className: 'mt-1 text-sm text-gray-500 dark:text-gray-400 italic whitespace-pre-wrap', textContent: text }));
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
        if (!items.length) { list.innerHTML = '<p class="text-xs text-gray-400 dark:text-gray-500 py-2">No instances running</p>'; return; }

        const orgMap = new Map();
        for (const inst of items) {
            const org = inst.org || 'unknown';
            if (!orgMap.has(org)) orgMap.set(org, []);
            orgMap.get(org).push(inst);
        }

        const sortedOrgs = Array.from(orgMap.keys()).sort();
        const fragment = document.createDocumentFragment();

        for (const org of sortedOrgs) {
            const orgInstances = orgMap.get(org);
            const orgDiv = h('div', { className: 'mb-2' });
            const orgHeader = h('div', {
                className: 'flex items-center gap-1 px-2 py-1 text-xs font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wider',
            });
            orgHeader.appendChild(h('span', { textContent: org }));
            orgDiv.appendChild(orgHeader);

            for (const inst of orgInstances) {
                const el = createInstanceEl(inst);
                el.dataset.org = org;
                orgDiv.appendChild(el);
            }
            fragment.appendChild(orgDiv);
        }

        list.textContent = '';
        list.appendChild(fragment);
    }

    function createInstanceEl(inst) {
        const sel = inst.id === store.selectedInstanceId;
        const repo = inst.repo || inst.working_directory.split('/').pop() || 'unknown';
        const div = h('div', {
            className: `flex items-center gap-2 px-2 py-1.5 rounded cursor-pointer group ${sel ? 'bg-gray-200 dark:bg-gray-700' : 'hover:bg-gray-100 dark:hover:bg-gray-800'}`,
            'data-action': 'select-instance', 'data-id': inst.id,
        });
        div.appendChild(h('span', { className: `w-2 h-2 rounded-full ${statusColor(inst.status)} flex-shrink-0` }));
        div.appendChild(h('span', { className: 'text-sm truncate flex-1 text-gray-700 dark:text-gray-200', textContent: repo, title: inst.working_directory }));
        if (inst.last_heartbeat_at) {
            const relativeTime = formatRelativeTime(inst.last_heartbeat_at);
            const exactTime = formatTime(inst.last_heartbeat_at);
            div.appendChild(h('span', { className: 'text-xs text-gray-400 dark:text-gray-500 flex-shrink-0', title: exactTime, textContent: relativeTime }));
        }
        div.appendChild(h('button', {
            className: 'text-gray-500 dark:text-gray-400 hover:text-red-500 dark:hover:text-red-400 text-xs px-1 flex-shrink-0',
            'data-action': 'delete-instance', 'data-id': inst.id, textContent: '\u00d7',
        }));
        return div;
    }

    function updateInstanceEl(el, inst) {
        const sel = inst.id === store.selectedInstanceId;
        const repo = inst.repo || inst.working_directory.split('/').pop() || 'unknown';
        el.className = `flex items-center gap-2 px-2 py-1.5 rounded cursor-pointer group ${sel ? 'bg-gray-200 dark:bg-gray-700' : 'hover:bg-gray-100 dark:hover:bg-gray-800'}`;
        const dot = el.querySelector('span:first-child');
        if (dot) dot.className = `w-2 h-2 rounded-full ${statusColor(inst.status)} flex-shrink-0`;
        const children = Array.from(el.children);
        const textEl = children[1];
        if (textEl) {
            textEl.textContent = repo;
            textEl.title = inst.working_directory;
        }
        let hbEl = children[2];
        if (inst.last_heartbeat_at) {
            const relativeTime = formatRelativeTime(inst.last_heartbeat_at);
            const exactTime = formatTime(inst.last_heartbeat_at);
            if (hbEl && hbEl.tagName === 'SPAN' && !hbEl.dataset.action) {
                hbEl.textContent = relativeTime;
                hbEl.title = exactTime;
            } else {
                hbEl = h('span', { className: 'text-xs text-gray-400 dark:text-gray-500 flex-shrink-0', title: exactTime, textContent: relativeTime });
                if (children[2]) el.insertBefore(hbEl, children[2]);
                else el.appendChild(hbEl);
            }
        } else if (hbEl && hbEl.tagName === 'SPAN' && !hbEl.dataset.action) {
            hbEl.remove();
        }
    }

    function createInstanceEl(inst) {
        const sel = inst.id === store.selectedInstanceId;
        const div = h('div', {
            className: `flex items-center gap-2 px-2 py-1.5 rounded cursor-pointer group ${sel ? 'bg-gray-200 dark:bg-gray-700' : 'hover:bg-gray-100 dark:hover:bg-gray-800'}`,
            'data-action': 'select-instance', 'data-id': inst.id,
        });
        div.appendChild(h('span', { className: `w-2 h-2 rounded-full ${statusColor(inst.status)} flex-shrink-0` }));
        div.appendChild(h('span', { className: 'text-sm truncate flex-1 text-gray-700 dark:text-gray-200', textContent: inst.working_directory.split('/').pop() || inst.working_directory, title: inst.working_directory }));
        if (inst.last_heartbeat_at) {
            const relativeTime = formatRelativeTime(inst.last_heartbeat_at);
            const exactTime = formatTime(inst.last_heartbeat_at);
            div.appendChild(h('span', { className: 'text-xs text-gray-400 dark:text-gray-500 flex-shrink-0', title: exactTime, textContent: relativeTime }));
        }
        div.appendChild(h('button', {
            className: 'text-gray-500 dark:text-gray-400 hover:text-red-500 dark:hover:text-red-400 text-xs px-1 flex-shrink-0',
            'data-action': 'delete-instance', 'data-id': inst.id, textContent: '\u00d7',
        }));
        return div;
    }

    function updateInstanceEl(el, inst) {
        const sel = inst.id === store.selectedInstanceId;
        el.className = `flex items-center gap-2 px-2 py-1.5 rounded cursor-pointer group ${sel ? 'bg-gray-200 dark:bg-gray-700' : 'hover:bg-gray-100 dark:hover:bg-gray-800'}`;
        const dot = el.querySelector('span:first-child');
        if (dot) dot.className = `w-2 h-2 rounded-full ${statusColor(inst.status)} flex-shrink-0`;
        const children = Array.from(el.children);
        const textEl = children[1];
        if (textEl) {
            textEl.textContent = inst.working_directory.split('/').pop() || inst.working_directory;
            textEl.title = inst.working_directory;
        }
        let hbEl = children[2];
        if (inst.last_heartbeat_at) {
            const relativeTime = formatRelativeTime(inst.last_heartbeat_at);
            const exactTime = formatTime(inst.last_heartbeat_at);
            if (hbEl && hbEl.tagName === 'SPAN' && !hbEl.dataset.action) {
                hbEl.textContent = relativeTime;
                hbEl.title = exactTime;
            } else {
                hbEl = h('span', { className: 'text-xs text-gray-400 dark:text-gray-500 flex-shrink-0', title: exactTime, textContent: relativeTime });
                if (children[2]) el.insertBefore(hbEl, children[2]);
                else el.appendChild(hbEl);
            }
        } else if (hbEl && hbEl.tagName === 'SPAN' && !hbEl.dataset.action) {
            hbEl.remove();
        }
    }

    function statusColor(s) {
        return { running: 'bg-green-500', starting: 'bg-yellow-500 animate-pulse', error: 'bg-red-500', stopped: 'bg-gray-500' }[s] || 'bg-gray-500';
    }

    function renderSessions() {
        const list = document.getElementById('session-list');
        if (!list) return;
        const items = Array.from(store.sessions.values());
        if (!items.length) { list.innerHTML = '<p class="text-xs text-gray-400 dark:text-gray-500 py-2">No sessions</p>'; return; }

        // Build parent -> children map
        const childrenMap = new Map();
        const rootSessions = [];

        for (const sess of items) {
            const pid = sess.parent_id || sess.parentID || '';
            if (!pid) {
                rootSessions.push(sess);
            } else {
                if (!childrenMap.has(pid)) childrenMap.set(pid, []);
                childrenMap.get(pid).push(sess);
            }
        }

        // Build flattened list with hierarchy info
        function flattenWithChildren(session, depth = 0) {
            const result = [{ session, depth }];
            const children = childrenMap.get(session.id) || [];
            for (const child of children) {
                result.push(...flattenWithChildren(child, depth + 1));
            }
            return result;
        }

        const flattened = [];
        for (const sess of rootSessions) {
            flattened.push(...flattenWithChildren(sess));
        }

        // Include any orphaned children (parent not in our list)
        for (const sess of items) {
            const pid = sess.parent_id || sess.parentID || '';
            if (pid && !store.sessions.has(pid)) {
                flattened.push({ session: sess, depth: 1 });
            }
        }

        reconcileList(list, flattened, item => item.session.id,
            item => createSessionEl(item.session, item.depth),
            (el, item) => updateSessionEl(el, item.session, item.depth));
    }

    function createSessionEl(sess, depth = 0) {
        const sel = sess.id === store.selectedSessionId;
        const title = sess.title || 'Untitled';
        const busy = sel && store.sessionBusy;
        const hasQuestion = sel && store.sessionQuestion;
        const indent = depth * 12;
        const isSubAgent = depth > 0;
        const div = h('div', {
            className: `flex items-center gap-2 px-2 py-1.5 rounded cursor-pointer text-sm ${sel ? 'bg-gray-200 dark:bg-gray-700 text-gray-900 dark:text-white' : 'text-gray-500 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-800'}`,
            'data-action': 'select-session', 'data-id': sess.id, title,
        });
        div.style.paddingLeft = `${8 + indent}px`;
        if (isSubAgent) {
            div.appendChild(h('span', { className: 'text-xs text-purple-400 dark:text-purple-500', textContent: '\u21b3' }));
        } else {
            let dotClass = 'bg-gray-400 dark:bg-gray-600';
            if (hasQuestion) {
                dotClass = 'bg-red-500 animate-pulse';
            } else if (busy) {
                dotClass = 'bg-yellow-400 animate-pulse';
            }
            div.appendChild(h('span', { className: `w-1.5 h-1.5 rounded-full flex-shrink-0 ${dotClass}` }));
        }
        div.appendChild(h('span', { className: 'truncate flex-1', textContent: title }));
        div.appendChild(h('button', {
            className: 'text-gray-500 dark:text-gray-400 hover:text-red-500 dark:hover:text-red-400 text-xs px-1 flex-shrink-0',
            'data-action': 'delete-session', 'data-id': sess.id, textContent: '\u00d7',
        }));
        return div;
    }

    function updateSessionEl(el, sess, depth = 0) {
        const sel = sess.id === store.selectedSessionId;
        const title = sess.title || 'Untitled';
        const busy = sel && store.sessionBusy;
        const hasQuestion = sel && store.sessionQuestion;
        const isSubAgent = depth > 0;
        el.style.paddingLeft = `${8 + depth * 12}px`;
        el.className = `flex items-center gap-2 px-2 py-1.5 rounded cursor-pointer text-sm ${sel ? 'bg-gray-200 dark:bg-gray-700 text-gray-900 dark:text-white' : 'text-gray-500 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-800'}`;
        el.title = title;
        const dot = el.querySelector('span:first-child');
        if (dot) {
            if (isSubAgent) {
                dot.className = 'text-xs text-purple-400 dark:text-purple-500';
                dot.textContent = '\u21b3';
            } else {
                let dotClass = 'bg-gray-400 dark:bg-gray-600';
                if (hasQuestion) {
                    dotClass = 'bg-red-500 animate-pulse';
                } else if (busy) {
                    dotClass = 'bg-yellow-400 animate-pulse';
                }
                dot.className = `w-1.5 h-1.5 rounded-full flex-shrink-0 ${dotClass}`;
            }
        }
        const children = Array.from(el.children);
        const titleEl = children[1];
        if (titleEl && titleEl.textContent !== title) titleEl.textContent = title;
        let delBtn = children[2];
        if (!delBtn || !delBtn.dataset.action) {
            delBtn = h('button', {
                className: 'text-gray-500 dark:text-gray-400 hover:text-red-500 dark:hover:text-red-400 text-xs px-1 flex-shrink-0',
                'data-action': 'delete-session', 'data-id': sess.id, textContent: '\u00d7',
            });
            if (children[2]) el.insertBefore(delBtn, children[2]);
            else el.appendChild(delBtn);
        }
    }

    // =========================================================================
    // Section 8b — Flock Agent UI
    // =========================================================================

    function renderFlockAgent() {
        const flockAgent = document.getElementById('flock-agent');
        const flockAgentEmpty = document.getElementById('flock-agent-empty');
        const btnRotate = document.getElementById('btn-flock-agent-rotate');
        const btnNew = document.getElementById('btn-new-flock-agent');
        if (!flockAgent || !flockAgentEmpty || !btnRotate || !btnNew) return;
        if (store.flockAgentActive) {
            flockAgent.classList.remove('hidden');
            flockAgentEmpty.classList.add('hidden');
            btnRotate.classList.remove('hidden');
            btnNew.classList.add('hidden');
        } else {
            flockAgent.classList.add('hidden');
            flockAgentEmpty.classList.remove('hidden');
            btnRotate.classList.add('hidden');
            btnNew.classList.remove('hidden');
        }
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
        store.viewingFlockAgent = false;
        store.sessions.clear(); store.messages.clear(); store.streamingParts.clear();
        store.sessionBusy = false; store.sessionBusyTool = null;
        closeEventSource();
        renderInstances(); renderSessions(); renderMessages(); updateHeader(); updateHeaderStatus();
        document.getElementById('btn-new-session').classList.remove('hidden');
        document.getElementById('input-area').classList.add('hidden');
        loadSessions(id);
        updateURL();
    }

    function selectFlockAgent() {
        store.selectedInstanceId = null;
        store.selectedSessionId = null;
        store.viewingFlockAgent = true;
        store.sessions.clear(); store.messages.clear(); store.streamingParts.clear();
        store.sessionBusy = false; store.sessionBusyTool = null;
        closeEventSource();
        renderInstances(); renderSessions(); renderMessages(); updateHeader(); updateHeaderStatus(); updateInputState();
        document.getElementById('btn-new-session').classList.add('hidden');
        document.getElementById('input-area').classList.remove('hidden');
        document.getElementById('main-header').textContent = 'Flock Agent';
        loadFlockAgentMessages();
        connectFlockAgentSSE();
    }

    function selectSession(id) {
        if (store.selectedSessionId === id) return;
        store.selectedSessionId = id;
        store.viewingFlockAgent = false;
        store.messages.clear(); store.streamingParts.clear();
        store.sessionBusy = false; store.sessionBusyTool = null;
        renderSessions(); renderMessages(); updateHeader(); updateHeaderStatus(); updateInputState();
        document.getElementById('input-area').classList.remove('hidden');
        loadMessages(id);
        connectSSE(id);
        updateURL();
    }

    function updateHeader() {
        const el = document.getElementById('main-header');
        if (!el) return;
        if (store.selectedSessionId) {
            el.textContent = store.sessions.get(store.selectedSessionId)?.title || 'Session';
        } else if (store.selectedInstanceId) {
            const inst = store.instances.get(store.selectedInstanceId);
            if (inst) {
                const orgRepo = inst.org && inst.repo ? `${inst.org}/${inst.repo}` : inst.working_directory.split('/').slice(-2).join('/');
                el.textContent = `Instance: ${orgRepo}`;
            } else {
                el.textContent = 'Instance';
            }
        } else { el.textContent = 'Select an instance to get started'; }
    }

    function updateHeaderStatus() {
        const el = document.getElementById('header-status');
        if (!el) return;
        if (!store.selectedSessionId) { el.innerHTML = ''; return; }
        if (store.sessionQuestion) {
            el.innerHTML = '<span class="w-2 h-2 rounded-full bg-red-500 animate-pulse inline-block"></span><span>Question pending - type your answer</span>';
        } else if (store.sessionBusy) {
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
        const canInput = !store.sessionBusy || store.sessionQuestion;
        btn.disabled = !canInput;
        btn.textContent = store.sessionQuestion ? 'Answer' : (store.sessionBusy ? 'Working\u2026' : 'Send');
        input.disabled = !canInput;
        if (canInput) input.focus();
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
            case 'delete-instance': e.stopPropagation(); if (confirm('Remove this instance?')) deleteInstance(t.dataset.id); break;
            case 'select-session': selectSession(t.dataset.id); break;
            case 'delete-session': e.stopPropagation(); if (confirm('Delete this session?')) deleteSession(t.dataset.id); break;
            case 'select-flock-agent': selectFlockAgent(); break;
        }
    });

    document.getElementById('btn-new-instance').addEventListener('click', () => {
        document.getElementById('modal-new-instance').classList.remove('hidden');
        document.getElementById('input-github-url').value = '';
        document.getElementById('input-github-url').focus();
    });
    document.getElementById('btn-cancel-instance').addEventListener('click', () => document.getElementById('modal-new-instance').classList.add('hidden'));
    document.getElementById('btn-create-instance').addEventListener('click', () => {
        const url = document.getElementById('input-github-url').value.trim();
        if (!url) return;
        document.getElementById('modal-new-instance').classList.add('hidden');
        createInstance(url);
    });
    document.getElementById('input-github-url').addEventListener('keydown', (e) => {
        if (e.key === 'Enter') { e.preventDefault(); document.getElementById('btn-create-instance').click(); }
    });
    document.getElementById('btn-new-session').addEventListener('click', () => {
        if (store.selectedInstanceId) createSession(store.selectedInstanceId);
    });
    document.getElementById('btn-new-flock-agent').addEventListener('click', () => {
        createFlockAgent();
    });
    document.getElementById('btn-flock-agent-rotate').addEventListener('click', () => {
        if (confirm('Rotate session? This will start a new conversation.')) rotateFlockAgent();
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
            return `<code class="bg-gray-200 dark:bg-gray-700 text-gray-800 dark:text-gray-200 px-1.5 py-0.5 rounded text-sm">${t}</code>`;
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
    initTheme();
    initMarkdown();
    initResize();
    refreshInstances();
    refreshFlockAgent();
    restoreFromURL();
    setInterval(refreshInstances, 5000);
    setInterval(refreshSessions, 5000);
    setInterval(refreshFlockAgent, 5000);

    document.getElementById('btn-theme-toggle').addEventListener('click', toggleTheme);
})();
