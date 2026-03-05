// Flock - Agent Orchestration UI

const state = {
    instances: [],
    sessions: [],
    selectedInstance: null,
    selectedSession: null,
    messages: [],
    eventSource: null,
    streaming: false,
};

// API helpers
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

// Instance operations
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

// Session operations
async function loadSessions(instanceId) {
    state.sessions = await api('GET', `/api/instances/${instanceId}/sessions`) || [];
    renderSessions();
}

async function createSession(instanceId) {
    const session = await api('POST', `/api/instances/${instanceId}/sessions`);
    await loadSessions(instanceId);
    selectSession(session.id);
}

async function loadMessages(sessionId) {
    state.messages = await api('GET', `/api/sessions/${sessionId}/messages`) || [];
    renderMessages();
}

async function sendMessage(sessionId, content) {
    await api('POST', `/api/sessions/${sessionId}/messages`, { content });
    // Messages will arrive via SSE
}

// Selection
function selectInstance(id) {
    state.selectedInstance = id;
    state.selectedSession = null;
    state.messages = [];
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

// SSE
function subscribeEvents(sessionId) {
    if (state.eventSource) {
        state.eventSource.close();
    }
    state.eventSource = new EventSource(`/api/sessions/${sessionId}/events`);

    state.eventSource.addEventListener('message.part.updated', (e) => {
        const data = JSON.parse(e.data);
        handleStreamingUpdate(data);
    });

    state.eventSource.addEventListener('session.updated', (e) => {
        loadMessages(sessionId);
    });

    state.eventSource.addEventListener('session.idle', () => {
        state.streaming = false;
        loadMessages(sessionId);
    });

    state.eventSource.onerror = () => {
        console.warn('SSE connection error, will retry...');
    };
}

function handleStreamingUpdate(data) {
    state.streaming = true;
    const messagesEl = document.getElementById('messages');
    let streamEl = document.getElementById('streaming-message');
    if (!streamEl) {
        streamEl = document.createElement('div');
        streamEl.id = 'streaming-message';
        streamEl.className = 'flex justify-start';
        streamEl.innerHTML = '<div class="max-w-2xl bg-gray-800 rounded-lg px-4 py-3 text-gray-100"><pre class="whitespace-pre-wrap font-mono text-sm"></pre></div>';
        messagesEl.appendChild(streamEl);
    }
    const pre = streamEl.querySelector('pre');
    if (data.content) {
        pre.textContent = data.content;
    } else if (data.text) {
        pre.textContent = data.text;
    }
    messagesEl.scrollTop = messagesEl.scrollHeight;
}

// Rendering
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
            <div class="px-2 py-1.5 rounded cursor-pointer text-sm truncate ${selected ? 'bg-gray-700' : 'hover:bg-gray-800'}"
                 onclick="selectSession('${id}')"
                 title="${title}">
                ${title || id.slice(0, 8)}
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
    container.innerHTML = state.messages.map(msg => {
        const isUser = msg.role === 'user';
        const align = isUser ? 'justify-end' : 'justify-start';
        const bg = isUser ? 'bg-blue-600' : 'bg-gray-800';
        const content = getMessageContent(msg);
        return `
            <div class="flex ${align}">
                <div class="max-w-2xl ${bg} rounded-lg px-4 py-3 text-gray-100">
                    <pre class="whitespace-pre-wrap font-mono text-sm">${escapeHtml(content)}</pre>
                </div>
            </div>
        `;
    }).join('');
    container.scrollTop = container.scrollHeight;
}

function getMessageContent(msg) {
    if (!msg.parts || !msg.parts.length) return msg.content || '';
    return msg.parts.map(p => p.content || p.text || '').join('');
}

function escapeHtml(str) {
    const div = document.createElement('div');
    div.textContent = str;
    return div.innerHTML;
}

function updateHeader() {
    const header = document.getElementById('main-header');
    if (state.selectedSession) {
        const sess = state.sessions.find(s => (s.id || s.ID) === state.selectedSession);
        header.textContent = `Session: ${sess?.title || sess?.Title || state.selectedSession.slice(0, 8)}`;
    } else if (state.selectedInstance) {
        const inst = state.instances.find(i => i.id === state.selectedInstance);
        header.textContent = `Instance: ${inst?.working_directory || state.selectedInstance.slice(0, 8)}`;
    } else {
        header.textContent = 'Select an instance to get started';
    }
}

// Event listeners
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
    if (!content || !state.selectedSession) return;
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

// Initial load
loadInstances();

// Poll for instance status updates
setInterval(loadInstances, 5000);
