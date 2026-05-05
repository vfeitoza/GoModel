// OAuth provider management module
(function(global) {
    function dashboardOAuthModule() {
        return {
            oauthProviders: [],
            oauthUsage: {},
            oauthLoading: false,
            oauthUsageLoading: {},
            oauthActiveProvider: '',
            oauthRevokingProvider: '',
            oauthErrors: {},
            oauthGlobalError: '',

            async oauthInit() {
                await this.oauthLoadProviders();
            },

            async oauthLoadProviders() {
                this.oauthLoading = true;
                this.oauthGlobalError = '';
                try {
                    const request = typeof this.requestOptions === 'function'
                        ? this.requestOptions()
                        : { headers: this.headers() };
                    const res = await fetch('/admin/api/v1/oauth/providers', request);
                    if (typeof this.handleFetchResponse === 'function') {
                        const handled = this.handleFetchResponse(res, 'OAuth providers', request);
                        if (!handled) {
                            this.oauthGlobalError = 'Failed to load OAuth providers.';
                            return;
                        }
                    } else if (!res.ok) {
                        this.oauthGlobalError = 'Failed to load OAuth providers.';
                        return;
                    }
                    const payload = await res.json();
                    this.oauthProviders = payload.providers || [];
                    for (const p of this.oauthProviders) {
                        if (p.authenticated) {
                            this.oauthLoadUsage(p.provider_name);
                        }
                    }
                } catch (err) {
                    console.error('Failed to load OAuth providers:', err);
                    this.oauthGlobalError = 'Failed to load OAuth providers.';
                } finally {
                    this.oauthLoading = false;
                }
            },

            async oauthRefresh() {
                await this.oauthLoadProviders();
            },

            async oauthAuthenticate(providerName) {
                if (this.oauthActiveProvider) return;
                this.oauthActiveProvider = providerName;
                this.oauthErrors = Object.assign({}, this.oauthErrors, { [providerName]: '' });

                try {
                    const body = JSON.stringify({ provider_name: providerName });
                    const request = typeof this.requestOptions === 'function'
                        ? this.requestOptions({ method: 'POST', body })
                        : { method: 'POST', headers: this.headers(), body };
                    const res = await fetch('/admin/api/v1/oauth/start', request);
                    if (!res.ok) {
                        const msg = await this._oauthResponseMessage(res, 'Failed to start OAuth flow.');
                        this.oauthErrors = Object.assign({}, this.oauthErrors, { [providerName]: msg });
                        return;
                    }
                    const data = await res.json();
                    const authURL = data.auth_url;

                    const width = 600, height = 700;
                    const left = window.screenX + (window.outerWidth - width) / 2;
                    const top = window.screenY + (window.outerHeight - height) / 2;
                    const popup = window.open(
                        authURL,
                        'oauth_' + providerName,
                        'width=' + width + ',height=' + height + ',left=' + left + ',top=' + top + ',toolbar=no,menubar=no'
                    );

                    if (!popup) {
                        this.oauthErrors = Object.assign({}, this.oauthErrors, { [providerName]: 'Popup blocked — please allow popups for this site.' });
                        return;
                    }

                    await this._oauthWaitForCallback(popup);
                    this.oauthErrors = Object.assign({}, this.oauthErrors, { [providerName]: '' });
                    await this.oauthLoadProviders();
                } catch (err) {
                    console.error('OAuth authentication failed:', err);
                    this.oauthErrors = Object.assign({}, this.oauthErrors, { [providerName]: err.message || 'Authentication failed.' });
                } finally {
                    this.oauthActiveProvider = '';
                }
            },

            _oauthWaitForCallback(popup) {
                return new Promise((resolve, reject) => {
                    let resolved = false;

                    const timeout = setTimeout(() => {
                        if (resolved) return;
                        cleanup();
                        reject(new Error('Authentication timeout — please try again'));
                    }, 3 * 60 * 1000);

                    const checkClosed = setInterval(() => {
                        if (resolved) return;
                        if (popup.closed) {
                            // Give postMessage a chance to arrive before rejecting
                            setTimeout(() => {
                                if (!resolved) {
                                    cleanup();
                                    reject(new Error('Authentication cancelled'));
                                }
                            }, 500);
                            clearInterval(checkClosed);
                        }
                    }, 500);

                    const messageHandler = (event) => {
                        if (event.data && event.data.type === 'gomodel-oauth-success') {
                            resolved = true;
                            cleanup();
                            resolve();
                        }
                    };

                    const cleanup = () => {
                        clearTimeout(timeout);
                        clearInterval(checkClosed);
                        window.removeEventListener('message', messageHandler);
                        if (!popup.closed) popup.close();
                    };

                    window.addEventListener('message', messageHandler);
                });
            },

            async oauthRevoke(providerName) {
                if (!confirm('Revoke OAuth access for ' + providerName + '?\n\nThis will remove the stored token. You\'ll need to re-authenticate to use this provider.')) {
                    return;
                }

                this.oauthRevokingProvider = providerName;
                this.oauthErrors = Object.assign({}, this.oauthErrors, { [providerName]: '' });

                try {
                    const body = JSON.stringify({ provider_name: providerName });
                    const request = typeof this.requestOptions === 'function'
                        ? this.requestOptions({ method: 'POST', body })
                        : { method: 'POST', headers: this.headers(), body };
                    const res = await fetch('/admin/api/v1/oauth/revoke', request);
                    if (!res.ok) {
                        const msg = await this._oauthResponseMessage(res, 'Failed to revoke OAuth access.');
                        this.oauthErrors = Object.assign({}, this.oauthErrors, { [providerName]: msg });
                        return;
                    }
                    const newUsage = Object.assign({}, this.oauthUsage);
                    delete newUsage[providerName];
                    this.oauthUsage = newUsage;
                    await this.oauthLoadProviders();
                } catch (err) {
                    console.error('Failed to revoke OAuth:', err);
                    this.oauthErrors = Object.assign({}, this.oauthErrors, { [providerName]: err.message || 'Failed to revoke.' });
                } finally {
                    this.oauthRevokingProvider = '';
                }
            },

            async oauthLoadUsage(providerName) {
                this.oauthUsageLoading = Object.assign({}, this.oauthUsageLoading, { [providerName]: true });
                try {
                    const request = typeof this.requestOptions === 'function'
                        ? this.requestOptions()
                        : { headers: this.headers() };
                    const res = await fetch('/admin/api/v1/oauth/usage/' + encodeURIComponent(providerName), request);
                    if (!res.ok) return;
                    const usage = await res.json();
                    this.oauthUsage = Object.assign({}, this.oauthUsage, { [providerName]: usage });
                } catch (err) {
                    console.error('Failed to load OAuth usage:', err);
                } finally {
                    this.oauthUsageLoading = Object.assign({}, this.oauthUsageLoading, { [providerName]: false });
                }
            },

            async _oauthResponseMessage(res, fallback) {
                try {
                    const payload = await res.json();
                    if (payload && payload.error) return payload.error;
                    if (payload && payload.message) return payload.message;
                } catch (_) {}
                return fallback;
            },

            oauthCardClass(provider) {
                if (!provider.authenticated) return 'oauth-card-pending';
                if (provider.status === 'expired') return 'oauth-card-expired';
                return 'oauth-card-authenticated';
            },

            oauthStatusPillClass(provider) {
                return 'oauth-status-' + (provider.status || 'pending');
            },

            oauthStatusLabel(provider) {
                const labels = { pending: 'Pending', authenticated: 'Authenticated', expired: 'Expired', error: 'Error' };
                return labels[provider.status || 'pending'] || provider.status;
            },

            oauthSubscriptionLabel(type) {
                if (!type) return 'Free';
                return type.charAt(0).toUpperCase() + type.slice(1);
            },

            oauthExpiresLabel(expiresAt) {
                if (!expiresAt) return 'Unknown';
                const diff = new Date(expiresAt) - new Date();
                if (diff < 0) return 'Expired';
                const h = Math.floor(diff / 3600000), d = Math.floor(h / 24);
                if (d > 0) return 'in ' + d + 'd ' + (h % 24) + 'h';
                if (h > 0) return 'in ' + h + 'h';
                return 'in ' + Math.floor(diff / 60000) + 'm';
            },

            oauthResetLabel(timestamp) {
                if (!timestamp) return '';
                const diff = new Date(timestamp) - new Date();
                if (diff < 0) {
                    const ago = -diff, m = Math.floor(ago / 60000);
                    if (m < 60) return m + 'm ago';
                    const h = Math.floor(m / 60);
                    if (h < 24) return h + 'h ago';
                    return Math.floor(h / 24) + 'd ago';
                }
                const h = Math.floor(diff / 3600000), d = Math.floor(h / 24);
                if (d > 0) return 'in ' + d + 'd ' + (h % 24) + 'h';
                if (h > 0) return 'in ' + h + 'h';
                return 'in ' + Math.floor(diff / 60000) + 'm';
            },

            oauthProgressClass(percent) {
                if (percent >= 90) return 'progress-danger';
                if (percent >= 70) return 'progress-warning';
                return 'progress-success';
            },

            oauthExtraUsageLabel(extra) {
                if (!extra || !extra.is_enabled) return 'Disabled';
                if (extra.used_credits != null && extra.monthly_limit != null)
                    return extra.used_credits.toFixed(0) + ' / ' + extra.monthly_limit.toFixed(0);
                if (extra.utilization != null) return Math.round(extra.utilization * 100) + '%';
                return 'Enabled';
            }
        };
    }

    global.dashboardOAuthModule = dashboardOAuthModule;
})(window);
