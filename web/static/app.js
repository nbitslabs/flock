// Flock - Agent Orchestration UI
(function() {
    'use strict';

    const state = {
        instances: [],
        sessions: [],
        selectedInstance: null,
        selectedSession: null,
        messages: [],
        eventSource: null,
        streamingParts: new Map(), // partID -> { type, text }
        busy: false,
    };

    // --- API ---

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

    // --- Instances ---

    async function loadInstances() {
        try {
            state.instances = await api('GET', '/api/instances') || [];
        } catch (e) {
            console.error('loadInstances:', e);
            state.instances = [];
        }
        renderInstances();
    }

    async function createInstance(workDir) {
        try {
            await api('POST', '/api/instances', { working_directory: workDir });
            await loadInstances();
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
        if (state.selectedInstance === id) {
            state.selectedInstance = null;
            state.selectedSession = null;
            state.sessions = [];
            state.messages = [];
            closeEventSource();
            renderSessions();
            renderChat();
            updateHeader();
        }
        await loadInstances();
    }

    // --- Sessions ---

    async function loadSessions(instanceId) {
        try {
            state.sessions = await api('GET', `/api/instances/${instanceId}/sessions`) || [];
        } catch (e) {
            console.error('loadSessions:', e);
            state.sessions = [];
        }
        renderSessions();
    }

    async function createSession(instanceId) {
        const inst = state.instances.find(i => i.id === instanceId);
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

    // --- Messages ---

    async function loadMessages(sessionId) {
        try {
            const msgs = await api('GET', `/api/sessions/${sessionId}/messages`) || [];
            state.messages = msgs;
            state.streamingParts.clear();
            renderChat();
        } catch (e) {
            console.error('loadMessages:', e);
        }
    }

    async function sendMessage(sessionId, content) {
        if (state.busy) return;
        state.busy = true;
        updateInput();

        try {
            await api('POST', `/api/sessions/${sessionId}/messages`, { content });
        } catch (e) {
            console.error('sendMessage:', e);
            alert('Failed to send message: ' + e.message);
            state.busy = false;
            updateInput();
        }
        // Response arrives via SSE events
    }

    // --- Selection ---

    function selectInstance(id) {
        state.selectedInstance = id;
        state.selectedSession = null;
        state.sessions = [];
        state.messages = [];
        state.streamingParts.clear();
        closeEventSource();
        renderInstances();
        renderSessions();
        renderChat();
        updateHeader();
        document.getElementById('btn-new-session').classList.remove('hidden');
        loadSessions(id);
    }

    function selectSession(id) {
        if (state.selectedSession === id) return;
        state.selectedSession = id;
        state.messages = [];
        state.streamingParts.clear();
        state.busy = false;
        renderSessions();
        renderChat();
        updateHeader();
        updateInput();
        document.getElementById('input-area').classList.remove('hidden');
        loadMessages(id);
        connectSSE(id);
    }

    // --- SSE ---

    function closeEventSource() {
        if (state.eventSource) {
            state.eventSource.close();
            state.eventSource = null;
        }
    }

    function connectSSE(sessionId) {
        closeEventSource();
        const es = new EventSource(`/api/sessions/${sessionId}/events`);
        state.eventSource = es;

        es.onmessage = function(e) {
            // Ignore if we switched sessions
            if (state.selectedSession !== sessionId) {
                es.close();
                return;
            }
            try {
                const event = JSON.parse(e.data);
                routeEvent(event);
            } catch (err) {
                // ignore
            }
        };

        es.onerror = function() {
            // EventSource auto-reconnects
        };
    }

    function routeEvent(event) {
        const type = event.type;
        const props = event.properties || {};

        switch (type) {
            case 'message.part.delta':
                onPartDelta(props);
                break;
            case 'message.part.updated':
                onPartUpdated(props);
                break;
            case 'session.status':
                onSessionStatus(props);
                break;
            case 'session.updated':
                onSessionUpdated(props);
                break;
            case 'session.idle':
                onSessionIdle();
                break;
        }
    }

    function onPartDelta(props) {
        const { partID, delta, field, messageID } = props;
        if (!partID || field !== 'text' || delta === undefined) return;

        if (!state.streamingParts.has(partID)) {
            state.streamingParts.set(partID, { type: 'text', text: '', messageID });
        }
        const part = state.streamingParts.get(partID);
        part.text += delta;
        renderStreamingBubble();
    }

    function onPartUpdated(props) {
        const part = props.part;
        if (!part || !part.id) return;
        state.streamingParts.set(part.id, {
            type: part.type || 'text',
            text: part.text || '',
            messageID: part.messageID,
        });
        renderStreamingBubble();
    }

    function onSessionStatus(props) {
        const statusType = props.status?.type;
        if (statusType === 'busy') {
            state.busy = true;
            updateInput();
        } else if (statusType === 'idle') {
            state.busy = false;
            updateInput();
        }
    }

    function onSessionUpdated(props) {
        const info = props.info;
        if (!info) return;
        const sess = state.sessions.find(s => s.id === info.id);
        if (sess && info.title && info.title !== sess.title) {
            sess.title = info.title;
            renderSessions();
            if (state.selectedSession === info.id) updateHeader();
        }
    }

    function onSessionIdle() {
        state.busy = false;
        updateInput();
        // Reload full messages to get final clean state
        if (state.selectedSession) {
            loadMessages(state.selectedSession);
        }
    }

    // --- Rendering ---

    function renderInstances() {
        const list = document.getElementById('instance-list');
        if (!state.instances.length) {
            list.innerHTML = '<p class="text-xs text-gray-500 py-2">No instances running</p>';
            return;
        }
        list.innerHTML = state.instances.map(inst => {
            const selected = inst.id === state.selectedInstance;
            const colors = { running: 'bg-green-500', starting: 'bg-yellow-500 animate-pulse', error: 'bg-red-500', stopped: 'bg-gray-500' };
            const color = colors[inst.status] || 'bg-gray-500';
            const dir = inst.working_directory.split('/').pop() || inst.working_directory;
            return `
                <div class="flex items-center gap-2 px-2 py-1.5 rounded cursor-pointer ${selected ? 'bg-gray-700' : 'hover:bg-gray-800'}"
                     data-action="select-instance" data-id="${inst.id}">
                    <span class="w-2 h-2 rounded-full ${color} flex-shrink-0"></span>
                    <span class="text-sm truncate flex-1" title="${esc(inst.working_directory)}">${esc(dir)}</span>
                    <button class="text-gray-500 hover:text-red-400 text-xs px-1" data-action="delete-instance" data-id="${inst.id}">&times;</button>
                </div>`;
        }).join('');
    }

    function renderSessions() {
        const list = document.getElementById('session-list');
        if (!state.sessions.length) {
            list.innerHTML = '<p class="text-xs text-gray-500 py-2">No sessions</p>';
            return;
        }
        list.innerHTML = state.sessions.map(sess => {
            const id = sess.id;
            const title = sess.title || 'Untitled';
            const selected = id === state.selectedSession;
            return `
                <div class="px-2 py-1.5 rounded cursor-pointer text-sm truncate ${selected ? 'bg-gray-700 text-white' : 'text-gray-400 hover:bg-gray-800'}"
                     data-action="select-session" data-id="${id}" title="${esc(title)}">
                    ${esc(title)}
                </div>`;
        }).join('');
    }

    function renderChat() {
        const container = document.getElementById('messages');
        if (!state.messages.length && !state.streamingParts.size) {
            container.innerHTML = '<div class="flex items-center justify-center h-full text-gray-600"><p>No messages yet.</p></div>';
            return;
        }

        let html = '';
        for (const msg of state.messages) {
            html += renderMessageBubble(msg);
        }
        container.innerHTML = html;
        renderStreamingBubble();
        scrollToBottom();
    }

    function renderMessageBubble(msg) {
        const role = msg.info?.role || 'assistant';
        const isUser = role === 'user';
        const parts = msg.parts || [];

        const textContent = [];
        for (const p of parts) {
            if (p.type === 'text' && p.text) {
                textContent.push(esc(p.text));
            }
        }
        if (!textContent.length) return '';

        const align = isUser ? 'justify-end' : 'justify-start';
        const bg = isUser ? 'bg-blue-600' : 'bg-gray-800';
        return `<div class="flex ${align}">
            <div class="max-w-3xl ${bg} rounded-lg px-4 py-3">
                <pre class="whitespace-pre-wrap text-sm leading-relaxed">${textContent.join('\n')}</pre>
            </div>
        </div>`;
    }

    function renderStreamingBubble() {
        const container = document.getElementById('messages');
        let el = document.getElementById('streaming-bubble');

        // Collect text from streaming parts
        const texts = [];
        for (const [, part] of state.streamingParts) {
            if (part.type === 'text' && part.text) {
                texts.push(part.text);
            }
        }

        if (!texts.length) {
            if (el) el.remove();
            return;
        }

        if (!el) {
            el = document.createElement('div');
            el.id = 'streaming-bubble';
            el.className = 'flex justify-start';
            container.appendChild(el);
        }

        el.innerHTML = `
            <div class="max-w-3xl bg-gray-800 rounded-lg px-4 py-3 border border-gray-700">
                <pre class="whitespace-pre-wrap text-sm leading-relaxed">${esc(texts.join('\n'))}</pre>
                <span class="inline-block w-1.5 h-4 bg-blue-400 animate-pulse ml-0.5 align-text-bottom"></span>
            </div>`;
        scrollToBottom();
    }

    function updateHeader() {
        const el = document.getElementById('main-header');
        if (state.selectedSession) {
            const sess = state.sessions.find(s => s.id === state.selectedSession);
            el.textContent = sess?.title || 'Session';
        } else if (state.selectedInstance) {
            const inst = state.instances.find(i => i.id === state.selectedInstance);
            el.textContent = inst ? `Instance: ${inst.working_directory}` : 'Instance';
        } else {
            el.textContent = 'Select an instance to get started';
        }
    }

    function updateInput() {
        const btn = document.getElementById('btn-send');
        const input = document.getElementById('message-input');
        btn.disabled = state.busy;
        btn.textContent = state.busy ? 'Working...' : 'Send';
        input.disabled = state.busy;
        if (!state.busy) input.focus();
    }

    function scrollToBottom() {
        const el = document.getElementById('messages');
        el.scrollTop = el.scrollHeight;
    }

    function esc(s) {
        const d = document.createElement('div');
        d.textContent = s || '';
        return d.innerHTML;
    }

    // --- Event delegation ---

    document.addEventListener('click', function(e) {
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

    document.getElementById('btn-new-instance').addEventListener('click', () => {
        document.getElementById('modal-new-instance').classList.remove('hidden');
        document.getElementById('input-workdir').value = '';
        document.getElementById('input-workdir').focus();
    });

    document.getElementById('btn-cancel-instance').addEventListener('click', () => {
        document.getElementById('modal-new-instance').classList.add('hidden');
    });

    document.getElementById('btn-create-instance').addEventListener('click', () => {
        const workDir = document.getElementById('input-workdir').value.trim();
        if (!workDir) return;
        document.getElementById('modal-new-instance').classList.add('hidden');
        createInstance(workDir);
    });

    document.getElementById('input-workdir').addEventListener('keydown', (e) => {
        if (e.key === 'Enter') {
            e.preventDefault();
            document.getElementById('btn-create-instance').click();
        }
    });

    document.getElementById('btn-new-session').addEventListener('click', () => {
        if (state.selectedInstance) createSession(state.selectedInstance);
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

    function doSend() {
        const input = document.getElementById('message-input');
        const content = input.value.trim();
        if (!content || !state.selectedSession || state.busy) return;
        input.value = '';
        // Reset textarea height
        input.style.height = 'auto';
        sendMessage(state.selectedSession, content);
    }

    // Auto-resize textarea
    document.getElementById('message-input').addEventListener('input', function() {
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

    // --- Init ---
    loadInstances();
    setInterval(loadInstances, 5000);

})();
