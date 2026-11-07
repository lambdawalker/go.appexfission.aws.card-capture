'use strict';

const signin = document.querySelector('#signin');
const signout = document.querySelector('#signout');
const invite = document.querySelector('#invite');
const email = document.querySelector('#email');
const send = document.querySelector('#send');
const status = document.querySelector('#status');
let accessToken;

function base64url(bytes) {
    return btoa(String.fromCharCode(...new Uint8Array(bytes)))
        .replaceAll('+', '-').replaceAll('/', '_').replace(/=+$/, '');
}

async function beginSignIn(config) {
    const verifier = base64url(crypto.getRandomValues(new Uint8Array(32)));
    const state = base64url(crypto.getRandomValues(new Uint8Array(24)));
    const challenge = base64url(await crypto.subtle.digest('SHA-256', new TextEncoder().encode(verifier)));
    sessionStorage.setItem('demo-signin', JSON.stringify({verifier, state}));
    const params = new URLSearchParams({
        response_type: 'code', client_id: config.clientId, redirect_uri: config.redirect,
        scope: 'openid demo-invites/write', code_challenge_method: 'S256', code_challenge: challenge, state,
    });
    location.assign(`${config.login}/oauth2/authorize?${params}`);
}

async function initialize() {
    const callback = new URLSearchParams(location.search);
    if (location.search) history.replaceState(null, '', '/');
    const reply = await fetch('/config.json');
    if (!reply.ok) throw new Error('configuration');
    const config = await reply.json();
    signin.hidden = false;
    signin.onclick = () => beginSignIn(config).catch(() => {
        status.textContent = 'Could not start sign-in. Please try again.';
    });
    signout.onclick = () => {
        accessToken = undefined;
        const params = new URLSearchParams({client_id: config.clientId, logout_uri: config.redirect});
        location.assign(`${config.login}/logout?${params}`);
    };
    if (!callback.has('code') && !callback.has('error')) {
        status.textContent = 'Sign in with your invitation administrator account.';
        return;
    }
    const pending = JSON.parse(sessionStorage.getItem('demo-signin') || 'null');
    sessionStorage.removeItem('demo-signin');
    if (!pending || pending.state !== callback.get('state') || callback.has('error')) {
        status.textContent = 'Sign-in could not be verified. Please try again.';
        return;
    }
    const token = await fetch(`${config.login}/oauth2/token`, {
        method: 'POST',
        body: new URLSearchParams({
            grant_type: 'authorization_code', client_id: config.clientId, redirect_uri: config.redirect,
            code: callback.get('code'), code_verifier: pending.verifier,
        }),
    });
    if (!token.ok) throw new Error('sign-in');
    accessToken = (await token.json()).access_token;
    const session = await fetch('/api/session', {headers: {Authorization: `Bearer ${accessToken}`}});
    signin.hidden = true;
    signout.hidden = false;
    invite.hidden = !session.ok;
    status.textContent = session.ok ? '' : 'You signed in, but only invitation administrators can send invitations.';
}

invite.onsubmit = async event => {
    event.preventDefault();
    send.disabled = true;
    status.textContent = 'Sending invitation…';
    try {
        const result = await fetch('/api/invitations', {
            method: 'POST',
            headers: {Authorization: `Bearer ${accessToken}`, 'Content-Type': 'application/json'},
            body: JSON.stringify({email: email.value}),
        });
        if (result.ok) {
            invite.reset();
            status.textContent = 'Invitation requested. Cognito will email the tester a temporary password.';
        } else {
            const messages = {
                400: 'Enter a valid email address.',
                401: 'Your session expired. Sign out and sign in again.',
                403: 'Only invitation administrators can send invitations.',
                409: 'That email already has an account.',
                429: 'Too many requests. Try again later.',
            };
            status.textContent = messages[result.status] || 'The invitation could not be sent.';
        }
    } catch {
        status.textContent = 'Could not reach the invitation service. Try again.';
    } finally {
        send.disabled = false;
    }
};

initialize().catch(() => {
    status.textContent = 'Could not load the invitation console. Reload or try signing in again.';
});
