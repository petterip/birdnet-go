const clientId = 'birdnet-client';
const redirectUri = 'http://localhost:8080/callback';

function login() {
    window.location.href = `/oauth2/authorize?client_id=${clientId}&redirect_uri=${encodeURIComponent(redirectUri)}`;
}

async function handleCallback() {
    const urlParams = new URLSearchParams(window.location.search);
    const code = urlParams.get('code');
    
    if (code) {
        const tokenResponse = await fetch('/oauth2/token', {
            method: 'POST',
            headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
            body: `grant_type=authorization_code&code=${code}&client_id=${clientId}&client_secret=${clientSecret}`
        });
        
        if (tokenResponse.ok) {
            const { access_token } = await tokenResponse.json();
            localStorage.setItem('access_token', access_token);
            window.location.href = '/settings/main';
        } else {
            console.error('Failed to exchange code for token');
        }
    }
}

// Call handleCallback when the page loads (for the callback page)
if (window.location.pathname === '/callback') {
    handleCallback();
}

function addAuthHeader(url, options = {}) {
    const token = localStorage.getItem('access_token');
    if (token && url.startsWith('/settings/')) {
        if (!options.headers) options.headers = {};
        options.headers['Authorization'] = `Bearer ${token}`;
    }
    return fetchWithCredentials(url, options);
}

function fetchWithCredentials(url, options = {}) {
    if (!options.credentials) {
        options.credentials = 'include';
    }
    return fetch(url, options);
}

function checkAuth(url) {
    if (url.startsWith('/settings/')) {
        return fetchWithCredentials(url, { credentials: 'include' })
            .then(response => {
                if (response.redirected) {
                    window.location.href = response.url;
                    return Promise.reject('Redirecting to login');
                }
                return response;
            });
    }
    return fetchWithCredentials(url);
}

function moveDatePicker(days) {
	const picker = document.getElementById('datePicker');

	const [yy, mm, dd] = picker.value.split('-');

	const d = new Date(yy, mm - 1, dd)
	d.setDate(d.getDate() + days);
	picker.value = d.toLocaleString('sv').split(' ')[0];
	picker.dispatchEvent(new Event('change'))
}

function renderChart(chartId, chartData) {
	const chart = echarts.init(document.getElementById(chartId));
	chart.setOption(chartData);

	window.addEventListener('resize', () => chart.resize());
}

function isNotArrowKey(event) {
	return !['ArrowLeft', 'ArrowRight'].includes(event.key);
}

htmx.on('htmx:afterSettle', function (event) {
    if (event.detail.target.id.endsWith('-content')) {
        // Find all chart containers in the newly loaded content and render them
        event.detail.target.querySelectorAll('[id$="-chart"]').forEach(function (chartContainer) {
            renderChart(chartContainer.id, chartContainer.dataset.chartOptions);
        });
    }
});
