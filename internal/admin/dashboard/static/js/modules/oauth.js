// OAuth provider management module
export function initOAuth(Alpine) {
    Alpine.data('oauthState', () => ({
        oauthProviders: [],
        oauthUsage: {},
        oauthLoading: false,
        oauthUsageLoading: {},
        oauthAuthenticating: {},
        oauthRevoking: {},
        oauthErrors: {},

        async oauthInit() {
            await this.oauthLoadProviders();
        },

        async oauthLoadProviders() {
            this.oauthLoading = true;
            this.oauthErrors = {};
            try {
                const resp = await this.apiGet('/oauth/providers');
                this.oauthProviders = resp.providers || [];
                // Auto-load usage for authenticated providers
                for (const p of this.oauthProviders) {
                    if (p.authenticated) {
                        this.oauthLoadUsage(p.provider_name);
                    }
                }
            } catch (err) {
                console.error('Failed to load OAuth providers:', err);
                this.showToast('Failed to load OAuth providers: ' + err.message, 'error');
            } finally {
                this.oauthLoading = false;
            }
        },

        async oauthRefresh() {
            await this.oauthLoadProviders();
        },

        async oauthAuthenticate(providerName) {
            this.oauthAuthenticating[providerName] = true;
            this.oauthErrors[providerName] = null;

            try {
                // Start OAuth flow
                const resp = await this.apiPost('/oauth/start', { provider_name: providerName });
                const authURL = resp.auth_url;
                const state = resp.state;

                // Open popup
                const width = 600;
                const height = 700;
                const left = window.screenX + (window.outerWidth - width) / 2;
                const top = window.screenY + (window.outerHeight - height) / 2;
                const popup = window.open(
                    authURL,
                    'oauth_' + providerName,
                    `width=${width},height=${height},left=${left},top=${top},toolbar=no,menubar=no`
                );

                if (!popup) {
                    throw new Error('Popup blocked — please allow popups for this site');
                }

                // Wait for callback
                await this.oauthWaitForCallback(popup, state, providerName);

                this.showToast('Authentication successful', 'success');
                await this.oauthLoadProviders();
            } catch (err) {
                console.error('OAuth authentication failed:', err);
                this.oauthErrors[providerName] = err.message;
                this.showToast('Authentication failed: ' + err.message, 'error');
            } finally {
                this.oauthAuthenticating[providerName] = false;
            }
        },

        oauthWaitForCallback(popup, state, providerName) {
            return new Promise((resolve, reject) => {
                const timeout = setTimeout(() => {
                    cleanup();
                    reject(new Error('Authentication timeout — please try again'));
                }, 3 * 60 * 1000); // 3 minutes

                const checkClosed = setInterval(() => {
                    if (popup.closed) {
                        cleanup();
                        reject(new Error('Authentication cancelled'));
                    }
                }, 500);

                const messageHandler = (event) => {
                    if (event.data && event.data.type === 'gomodel-oauth-success') {
                        cleanup();
                        resolve();
                    }
                };

                const cleanup = () => {
                    clearTimeout(timeout);
                    clearInterval(checkClosed);
                    window.removeEventListener('message', messageHandler);
                    if (!popup.closed) {
                        popup.close();
                    }
                };

                window.addEventListener('message', messageHandler);
            });
        },

        async oauthRevoke(providerName) {
            if (!confirm(`Revoke OAuth access for ${providerName}?\n\nThis will remove the stored token. You'll need to re-authenticate to use this provider.`)) {
                return;
            }

            this.oauthRevoking[providerName] = true;
            this.oauthErrors[providerName] = null;

            try {
                await this.apiPost('/oauth/revoke', { provider_name: providerName });
                this.showToast('OAuth access revoked', 'success');
                delete this.oauthUsage[providerName];
                await this.oauthLoadProviders();
            } catch (err) {
                console.error('Failed to revoke OAuth:', err);
                this.oauthErrors[providerName] = err.message;
                this.showToast('Failed to revoke: ' + err.message, 'error');
            } finally {
                this.oauthRevoking[providerName] = false;
            }
        },

        async oauthLoadUsage(providerName) {
            this.oauthUsageLoading[providerName] = true;
            try {
                const usage = await this.apiGet(`/oauth/usage/${providerName}`);
                this.oauthUsage[providerName] = usage;
            } catch (err) {
                console.error('Failed to load usage:', err);
                // Silently fail — usage may not be available for all accounts
            } finally {
                this.oauthUsageLoading[providerName] = false;
            }
        },

        oauthCardClass(provider) {
            if (!provider.authenticated) return 'oauth-card-pending';
            if (provider.status === 'expired') return 'oauth-card-expired';
            return 'oauth-card-authenticated';
        },

        oauthStatusPillClass(provider) {
            const status = provider.status || 'pending';
            return `oauth-status-${status}`;
        },

        oauthStatusLabel(provider) {
            const status = provider.status || 'pending';
            const labels = {
                pending: 'Pending',
                authenticated: 'Authenticated',
                expired: 'Expired',
                error: 'Error'
            };
            return labels[status] || status;
        },

        oauthSubscriptionLabel(type) {
            if (!type) return 'Free';
            return type.charAt(0).toUpperCase() + type.slice(1);
        },

        oauthExpiresLabel(expiresAt) {
            if (!expiresAt) return 'Unknown';
            const now = new Date();
            const expires = new Date(expiresAt);
            const diff = expires - now;
            if (diff < 0) return 'Expired';
            const hours = Math.floor(diff / (1000 * 60 * 60));
            const days = Math.floor(hours / 24);
            if (days > 0) return `in ${days}d ${hours % 24}h`;
            if (hours > 0) return `in ${hours}h`;
            const minutes = Math.floor(diff / (1000 * 60));
            return `in ${minutes}m`;
        },

        oauthResetLabel(timestamp) {
            if (!timestamp) return '';
            const now = new Date();
            const target = new Date(timestamp);
            const diff = target - now;
            if (diff < 0) {
                const ago = now - target;
                const minutes = Math.floor(ago / (1000 * 60));
                if (minutes < 60) return `${minutes}m ago`;
                const hours = Math.floor(minutes / 60);
                if (hours < 24) return `${hours}h ago`;
                const days = Math.floor(hours / 24);
                return `${days}d ago`;
            }
            const hours = Math.floor(diff / (1000 * 60 * 60));
            const days = Math.floor(hours / 24);
            if (days > 0) return `in ${days}d ${hours % 24}h`;
            if (hours > 0) return `in ${hours}h`;
            const minutes = Math.floor(diff / (1000 * 60));
            return `in ${minutes}m`;
        },

        oauthProgressClass(percent) {
            if (percent >= 90) return 'progress-danger';
            if (percent >= 70) return 'progress-warning';
            return 'progress-success';
        },

        oauthExtraUsageLabel(extra) {
            if (!extra || !extra.is_enabled) return 'Disabled';
            if (extra.used_credits != null && extra.monthly_limit != null) {
                return `${extra.used_credits.toFixed(0)} / ${extra.monthly_limit.toFixed(0)}`;
            }
            if (extra.utilization != null) {
                return `${Math.round(extra.utilization * 100)}%`;
            }
            return 'Enabled';
        }
    }));
}
