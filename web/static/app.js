// Flock - Agent Orchestration UI

const state = {
    instances: [],
    sessions: [],
    selectedInstance: null,
    selectedSession: null,
    messages: [],
    eventSource: null,
    // Streaming state: tracks in-flight assistant message parts
    // partID -> { messageID, type, text }
    streamingParts: {},
    streamingMessageID: null,
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
    state.instances = await api('GET', '/api/instances') || [];
    renderInstances();
}

async function createInstance(workDir) {
    const inst = await api('POST', '/api/instances', { working_directory: workDir });
    await loadInstances();
    selectInstance(inst.id);
}

async function deleteInstance(id) {
    await api('DELETE', `/api/instances/${id}`);
    if (state.selectedInstance === id) {
        state.selectedInstance = null;
        state.selectedSession = null;
        state.sessions = [];
        state.messages = [];
        renderMessages();
        renderSessions();
        updateHeader();
    }
    await loadInstances();
}

// --- Sessions ---

async function loadSessions(instanceId) {
    state.sessions = await api('GET', `/api/instances/${instanceId}/sessions`) || [];
    renderSessions();
}

async function createSession(instanceId) {
    const inst = state.instances.find(i => i.id === instanceId);
    if (inst && inst.status !== 'running') {
        alert(`Instance is still ${inst.status}. Wait for it to be running.`);
        return;
    }
    try {
        const session = await api('POST', `/api/instances/${instanceId}/sessions`);
        await loadSessions(instanceId);
        selectSession(session.id);
    } catch (err) {
        console.error('create session failed:', err);
        alert('Failed to create session: ' + err.message);
    }
}

// --- Messages ---

async function loadMessages(sessionId) {
    const msgs = await api('GET', `/api/sessions/${sessionId}/messages`) || [];
    state.messages = msgs;
    state.streamingParts = {};
    state.streamingMessageID = null;
    renderMessages();
}

async function sendMessage(sessionId, content) {
    // Optimistically add user message to UI
    state.messages.push({
        info: { id: 'pending-' + Date.now(), role: 'user', time: { created: Date.now() } },
        parts: [{ type: 'text', text: content }],
    });
    state.busy = true;
    renderMessages();
    updateSendButton();

    try {
        await api('POST', `/api/sessions/${sessionId}/messages`, { content });
    } catch (err) {
        console.error('send failed:', err);
        state.busy = false;
        updateSendButton();
    }
    // Real response arrives via SSE
}

// --- Selection ---

function selectInstance(id) {
    state.selectedInstance = id;
    state.selectedSession = null;
    state.messages = [];
    state.streamingParts = {};
    renderInstances();
    renderMessages();
    updateHeader();
    document.getElementById('btn-new-session').classList.remove('hidden');
    loadSessions(id);
}

function selectSession(id) {
    state.selectedSession = id;
    renderSessions();
    document.getElementById('input-area').classList.remove('hidden');
    updateHeader();
    loadMessages(id);
    subscribeEvents(id);
}

// --- SSE Event Handling ---

function subscribeEvents(sessionId) {
    if (state.eventSource) {
        state.eventSource.close();
    }

    state.eventSource = new EventSource(`/api/sessions/${sessionId}/events`);

    state.eventSource.onmessage = (e) => {
        try {
            const event = JSON.parse(e.data);
            handleEvent(event);
        } catch (err) {
            // ignore parse errors (heartbeats etc)
        }
    };

    state.eventSource.onerror = () => {
        console.warn('SSE connection error, will auto-retry');
    };
}

function handleEvent(event) {
    const type = event.type;
    const props = event.properties || {};

    switch (type) {
        case 'message.updated':
            handleMessageUpdated(props);
            break;
        case 'message.part.updated':
            handlePartUpdated(props);
            break;
        case 'message.part.delta':
            handlePartDelta(props);
            break;
        case 'session.status':
            handleSessionStatus(props);
            break;
        case 'session.updated':
            handleSessionUpdated(props);
            break;
        case 'session.idle':
            handleSessionIdle(props);
            break;
    }
}

function handleMessageUpdated(props) {
    const info = props.info;
    if (!info) return;

    // If this is a new assistant message, track it for streaming
    if (info.role === 'assistant' && !info.time?.completed) {
        state.streamingMessageID = info.id;
    }

    // If the message is completed, do a full refresh
    if (info.time?.completed) {
        if (info.id === state.streamingMessageID) {
            state.streamingMessageID = null;
            state.streamingParts = {};
        }
    }
}

function handlePartUpdated(props) {
    const part = props.part;
    if (!part) return;

    // Store/update the part snapshot
    state.streamingParts[part.id] = {
        messageID: part.messageID,
        type: part.type,
        text: part.text || '',
    };
    renderStreamingMessage();
}

function handlePartDelta(props) {
    const partID = props.partID;
    const delta = props.delta;
    const field = props.field;
    if (!partID || delta === undefined || field !== 'text') return;

    if (!state.streamingParts[partID]) {
        state.streamingParts[partID] = {
            messageID: props.messageID,
            type: 'text',
            text: '',
        };
    }
    state.streamingParts[partID].text += delta;
    renderStreamingMessage();
}

function handleSessionStatus(props) {
    if (props.status?.type === 'idle') {
        state.busy = false;
        updateSendButton();
        // Full reload to get final state
        if (state.selectedSession) {
            loadMessages(state.selectedSession);
        }
    } else if (props.status?.type === 'busy') {
        state.busy = true;
        updateSendButton();
    }
}

function handleSessionUpdated(props) {
    const info = props.info;
    if (!info) return;
    // Update session title in sidebar
    const sess = state.sessions.find(s => s.id === info.id);
    if (sess && info.title) {
        sess.title = info.title;
        renderSessions();
        if (state.selectedSession === info.id) {
            updateHeader();
        }
    }
}

function handleSessionIdle() {
    state.busy = false;
    updateSendButton();
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
        const statusColor = {
            running: 'bg-green-500',
            starting: 'bg-yellow-500',
            error: 'bg-red-500',
            stopped: 'bg-gray-500',
        }[inst.status] || 'bg-gray-500';
        const dir = inst.working_directory.split('/').pop() || inst.working_directory;
        return `
            <div class="flex items-center gap-2 px-2 py-1.5 rounded cursor-pointer ${selected ? 'bg-gray-700' : 'hover:bg-gray-800'}"
                 onclick="selectInstance('${inst.id}')">
                <span class="w-2 h-2 rounded-full ${statusColor} flex-shrink-0"></span>
                <span class="text-sm truncate flex-1" title="${inst.working_directory}">${dir}</span>
                <button class="text-gray-500 hover:text-red-400 text-xs" onclick="event.stopPropagation(); deleteInstance('${inst.id}')">×</button>
            </div>
        `;
    }).join('');
}

function renderSessions() {
    const list = document.getElementById('session-list');
    if (!state.sessions.length) {
        list.innerHTML = '<p class="text-xs text-gray-500 py-2">No sessions</p>';
        return;
    }
    list.innerHTML = state.sessions.map(sess => {
        const id = sess.id || sess.ID;
        const title = sess.title || sess.Title || 'Untitled';
        const selected = id === state.selectedSession;
        return `
            <div class="px-2 py-1.5 rounded cursor-pointer text-sm truncate ${selected ? 'bg-gray-700 text-white' : 'text-gray-400 hover:bg-gray-800'}"
                 onclick="selectSession('${id}')"
                 title="${title}">
                ${title}
            </div>
        `;
    }).join('');
}

function renderMessages() {
    const container = document.getElementById('messages');
    if (!state.messages.length) {
        container.innerHTML = '<div class="flex items-center justify-center h-full text-gray-600"><p>No messages yet.</p></div>';
        return;
    }
    const html = state.messages.map(msg => renderMessage(msg)).join('');
    container.innerHTML = html;
    // Append streaming message if exists
    renderStreamingMessage();
    scrollToBottom();
}

function renderMessage(msg) {
    const role = msg.info?.role || msg.role || 'assistant';
    const isUser = role === 'user';
    const parts = msg.parts || [];

    const content = parts.map(p => {
        if (p.type === 'text') return escapeHtml(p.text || p.content || '');
        if (p.type === 'tool-invocation' || p.type === 'tool-call') {
            const name = p.toolName || p.name || 'tool';
            return `<span class="text-yellow-400">[${escapeHtml(name)}]</span>`;
        }
        if (p.type === 'reasoning') {
            const text = p.text || '';
            if (!text) return '';
            return `<span class="text-gray-500 italic">${escapeHtml(text)}</span>`;
        }
        return '';
    }).filter(Boolean).join('\n');

    if (!content) return '';

    const align = isUser ? 'justify-end' : 'justify-start';
    const bg = isUser ? 'bg-blue-600' : 'bg-gray-800';

    return `
        <div class="flex ${align}">
            <div class="max-w-3xl ${bg} rounded-lg px-4 py-3">
                <pre class="whitespace-pre-wrap font-mono text-sm leading-relaxed">${content}</pre>
            </div>
        </div>
    `;
}

function renderStreamingMessage() {
    const container = document.getElementById('messages');
    // Remove old streaming element
    const old = document.getElementById('streaming-msg');
    if (old) old.remove();

    // Collect streaming parts in order
    const parts = Object.values(state.streamingParts);
    if (!parts.length) return;

    // Build content from parts
    const textParts = parts.filter(p => p.type === 'text' && p.text);
    const reasoningParts = parts.filter(p => p.type === 'reasoning' && p.text);

    let content = '';
    if (reasoningParts.length) {
        content += `<span class="text-gray-500 italic">${escapeHtml(reasoningParts.map(p => p.text).join(''))}</span>\n`;
    }
    if (textParts.length) {
        content += escapeHtml(textParts.map(p => p.text).join(''));
    }

    if (!content.trim()) return;

    const div = document.createElement('div');
    div.id = 'streaming-msg';
    div.className = 'flex justify-start';
    div.innerHTML = `
        <div class="max-w-3xl bg-gray-800 rounded-lg px-4 py-3 border border-gray-700">
            <pre class="whitespace-pre-wrap font-mono text-sm leading-relaxed">${content}</pre>
            ${state.busy ? '<div class="mt-2"><span class="inline-block w-2 h-2 bg-blue-400 rounded-full animate-pulse"></span></div>' : ''}
        </div>
    `;
    container.appendChild(div);
    scrollToBottom();
}

function scrollToBottom() {
    const container = document.getElementById('messages');
    container.scrollTop = container.scrollHeight;
}

function updateHeader() {
    const header = document.getElementById('main-header');
    if (state.selectedSession) {
        const sess = state.sessions.find(s => (s.id || s.ID) === state.selectedSession);
        header.textContent = sess?.title || sess?.Title || 'Session';
    } else if (state.selectedInstance) {
        const inst = state.instances.find(i => i.id === state.selectedInstance);
        header.textContent = `Instance: ${inst?.working_directory || ''}`;
    } else {
        header.textContent = 'Select an instance to get started';
    }
}

function updateSendButton() {
    const btn = document.getElementById('btn-send');
    const input = document.getElementById('message-input');
    if (state.busy) {
        btn.disabled = true;
        btn.textContent = 'Working...';
        input.disabled = true;
    } else {
        btn.disabled = false;
        btn.textContent = 'Send';
        input.disabled = false;
    }
}

function escapeHtml(str) {
    const div = document.createElement('div');
    div.textContent = str || '';
    return div.innerHTML;
}

// --- Event listeners ---

document.getElementById('btn-new-instance').addEventListener('click', () => {
    document.getElementById('modal-new-instance').classList.remove('hidden');
    document.getElementById('input-workdir').focus();
});

document.getElementById('btn-cancel-instance').addEventListener('click', () => {
    document.getElementById('modal-new-instance').classList.add('hidden');
});

document.getElementById('btn-create-instance').addEventListener('click', () => {
    const workDir = document.getElementById('input-workdir').value.trim();
    if (!workDir) return;
    document.getElementById('modal-new-instance').classList.add('hidden');
    document.getElementById('input-workdir').value = '';
    createInstance(workDir);
});

document.getElementById('btn-new-session').addEventListener('click', () => {
    if (state.selectedInstance) {
        createSession(state.selectedInstance);
    }
});

document.getElementById('btn-send').addEventListener('click', () => {
    const input = document.getElementById('message-input');
    const content = input.value.trim();
    if (!content || !state.selectedSession || state.busy) return;
    input.value = '';
    sendMessage(state.selectedSession, content);
});

document.getElementById('message-input').addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
        e.preventDefault();
        document.getElementById('btn-send').click();
    }
});

document.getElementById('input-workdir').addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
        document.getElementById('btn-create-instance').click();
    }
});

// --- Init ---
loadInstances();
setInterval(loadInstances, 5000);
