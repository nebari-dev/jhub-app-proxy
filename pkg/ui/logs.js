const logsContainer = document.getElementById('logs');
const title = document.getElementById('title');
const progressContainer = document.getElementById('progressContainer');
const commandText = document.getElementById('commandText');
const versionText = document.getElementById('versionText');
const logo = document.getElementById('logo');
const autoScrollToggle = document.getElementById('autoScrollToggle');
const elapsedTime = document.getElementById('elapsedTime');

let isReady = false;
let lastLogCount = 0;
let authErrorShown = false;
let logoLoaded = false;

// Get basePath from the global scope (set in HTML head)
// Falls back to default if not set
const basePath = window.basePath || '/_temp/jhub-app-proxy';

// API base is basePath + /api
const apiBase = basePath + '/api';

// Auto-scroll state (default: true)
let autoScrollEnabled = localStorage.getItem('autoScroll') !== 'false';

// Initialize toggle state
if (!autoScrollEnabled) {
    autoScrollToggle.classList.remove('active');
}

// Toggle handler
autoScrollToggle.addEventListener('click', function() {
    autoScrollEnabled = !autoScrollEnabled;
    this.classList.toggle('active');
    localStorage.setItem('autoScroll', autoScrollEnabled);
});

// Load logo
async function loadLogo() {
    try {
        logo.src = basePath + '/static/logo.png';
        logo.style.display = 'block'; // Show logo
        logoLoaded = true;
        return true;
    } catch (err) {
        console.error('Failed to load logo:', err);
    }
    return false;
}

function scrollToBottom() {
    if (autoScrollEnabled) {
        logsContainer.scrollTop = logsContainer.scrollHeight;
    }
}

function formatElapsedTime(seconds) {
    if (!seconds || seconds < 0) {
        return '(0:00)';
    }

    const hours = Math.floor(seconds / 3600);
    const minutes = Math.floor((seconds % 3600) / 60);
    const secs = Math.floor(seconds % 60);

    if (hours > 0) {
        return `(${hours}:${String(minutes).padStart(2, '0')}:${String(secs).padStart(2, '0')})`;
    } else {
        return `(${minutes}:${String(secs).padStart(2, '0')})`;
    }
}

function addLog(stream, line) {
    const firstPlaceholder = logsContainer.querySelector('.log-placeholder');
    if (firstPlaceholder) {
        logsContainer.innerHTML = '';
    }

    const div = document.createElement('div');
    div.className = 'log-line' + (stream === 'stderr' ? ' log-stderr' : '');
    div.textContent = line;
    logsContainer.appendChild(div);
    scrollToBottom();
}

async function checkAppStatus() {
    try {
        const response = await fetch(apiBase + '/logs/stats');

        // Check for authentication errors
        if (response.status === 403 || response.status === 401) {
            // Only show error once to avoid overwriting success states
            if (!authErrorShown) {
                authErrorShown = true;
                title.innerHTML = 'Access Denied - Authentication Required';
                title.classList.add('error');
                progressContainer.classList.add('hidden');
                commandText.textContent = 'Authentication required to view this page';
                logsContainer.innerHTML = '<div class="log-line log-stderr">403 Forbidden: You do not have permission to view these logs.</div>';
            }
            return;
        }

        if (!response.ok) {
            throw new Error(`HTTP ${response.status}: ${response.statusText}`);
        }

        // Check if response is JSON (OAuth redirect returns HTML with 200 OK)
        const contentType = response.headers.get('content-type');
        if (!contentType || !contentType.includes('application/json')) {
            // Not JSON - likely redirected to OAuth login page
            if (!authErrorShown) {
                authErrorShown = true;
                title.innerHTML = 'Access Denied - Authentication Required';
                title.classList.add('error');
                progressContainer.classList.add('hidden');
                commandText.textContent = 'Authentication required to view this page';
                logsContainer.innerHTML = '<div class="log-line log-stderr">403 Forbidden: You do not have permission to view these logs.</div>';
            }
            return;
        }

        // Authentication succeeded - reset error state and retry logo
        if (authErrorShown) {
            authErrorShown = false;
            title.classList.remove('error');
            title.innerHTML = 'Deploying your application';
            progressContainer.classList.remove('hidden');

            // Retry loading logo if it failed due to auth
            if (!logoLoaded) {
                loadLogo();
            }
        }

        const data = await response.json();

        if (data.process_info && data.process_info.command) {
            commandText.textContent = data.process_info.command.join(' ');
        }

        if (data.version) {
            versionText.textContent = 'jhub-app-proxy v' + data.version;
        } else {
            versionText.textContent = 'jhub-app-proxy';
        }

        if (data.process_state) {
            const state = data.process_state.state;

            // Update elapsed time (show even if uptime is 0)
            if (data.process_state.uptime !== undefined) {
                elapsedTime.textContent = formatElapsedTime(data.process_state.uptime);
            }

            if (state === 'running' && !isReady) {
                isReady = true;
                progressContainer.classList.add('hidden');
                title.innerHTML = 'Application ready, redirecting...';

                // Get the redirect URL from meta tag (injected by backend)
                // Falls back to calculating from current path if not present
                let appRoot = '/';
                const metaTag = document.querySelector('meta[name="app-redirect-url"]');
                if (metaTag) {
                    appRoot = metaTag.getAttribute('content');
                } else {
                    // Fallback: Calculate from current path (for backward compatibility)
                    const currentPath = window.location.pathname;
                    const tempPrefix = '/_temp/jhub-app-proxy';
                    if (currentPath.includes(tempPrefix)) {
                        const prefixEndIndex = currentPath.indexOf(tempPrefix);
                        if (prefixEndIndex > 0) {
                            appRoot = currentPath.substring(0, prefixEndIndex) + '/';
                        }
                    }
                }

                console.log('Redirecting to app:', appRoot);

                // Redirect to application root
                setTimeout(() => {
                    window.location.href = appRoot;
                }, 500); // Small delay to show "redirecting..." message
            } else if (state === 'failed') {
                title.innerHTML = 'Your app failed to deploy, please fix your mistakes!';
                title.classList.add('error');
                progressContainer.classList.add('hidden');
            }
        }
    } catch (err) {
        console.error('Failed to check status:', err);
    }
}

let isInitialLoad = true;
async function loadAllLogs() {
    try {
        const response = await fetch(apiBase + '/logs/all');

        // Check for authentication errors
        if (response.status === 403 || response.status === 401) {
            if (!authErrorShown) {
                authErrorShown = true;
                logsContainer.innerHTML = '<div class="log-line log-stderr">403 Forbidden: Authentication required to view logs.</div>';
            }
            isInitialLoad = false;
            return;
        }

        if (!response.ok) {
            throw new Error(`HTTP ${response.status}: ${response.statusText}`);
        }

        // Check if response is JSON (OAuth redirect returns HTML with 200 OK)
        const contentType = response.headers.get('content-type');
        if (!contentType || !contentType.includes('application/json')) {
            // Not JSON - likely redirected to OAuth login page
            if (!authErrorShown) {
                authErrorShown = true;
                logsContainer.innerHTML = '<div class="log-line log-stderr">403 Forbidden: Authentication required to view logs.</div>';
            }
            isInitialLoad = false;
            return;
        }

        // Authentication succeeded - clear any auth error in logs
        if (authErrorShown) {
            logsContainer.innerHTML = '';
        }

        const data = await response.json();

        if (data.logs && data.logs.length > 0) {
            logsContainer.innerHTML = '';
            data.logs.forEach(line => {
                const div = document.createElement('div');
                div.className = 'log-line';
                if (line.includes('[stderr]')) {
                    div.className = 'log-line log-stderr';
                }
                div.textContent = line;
                logsContainer.appendChild(div);
            });
            scrollToBottom();
        }
    } catch (err) {
        console.error('Failed to load historical logs:', err);
        addLog('stderr', 'Error loading historical logs: ' + err.message);
    }
    isInitialLoad = false;
}

async function fetchRecentLogs() {
    if (isInitialLoad) return;

    try {
        const response = await fetch(apiBase + '/logs?lines=100');

        // Stop polling if authentication fails
        if (response.status === 403 || response.status === 401) {
            return;
        }

        if (!response.ok) {
            return;
        }

        // Check if response is JSON (OAuth redirect returns HTML with 200 OK)
        const contentType = response.headers.get('content-type');
        if (!contentType || !contentType.includes('application/json')) {
            return;
        }

        const data = await response.json();

        if (data.logs && data.logs.length > 0) {
            if (data.stats.total_lines > lastLogCount) {
                data.logs.forEach(log => {
                    addLog(log.stream, log.line);
                });
                lastLogCount = data.stats.total_lines;
            }
        }
    } catch (err) {
        console.error('Error fetching logs:', err);
    }
}

// Copy functionality
function copyToClipboard(text, button) {
    navigator.clipboard.writeText(text).then(() => {
        // Show success feedback
        const originalText = button.innerHTML;
        button.classList.add('copied');
        button.innerHTML = `
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <polyline points="20 6 9 17 4 12"></polyline>
            </svg>
            Copied!
        `;

        // Reset after 2 seconds
        setTimeout(() => {
            button.classList.remove('copied');
            button.innerHTML = originalText;
        }, 2000);
    }).catch(err => {
        console.error('Failed to copy:', err);
    });
}

document.getElementById('copyCommand').addEventListener('click', function() {
    const command = commandText.textContent;
    copyToClipboard(command, this);
});

document.getElementById('copyLogs').addEventListener('click', function() {
    const logLines = Array.from(logsContainer.querySelectorAll('.log-line'))
        .map(line => line.textContent)
        .join('\n');
    copyToClipboard(logLines, this);
});

// Initial calls
loadLogo();
checkAppStatus();
loadAllLogs().then(() => {
    setInterval(fetchRecentLogs, 1000);
});
setInterval(checkAppStatus, 2000);
