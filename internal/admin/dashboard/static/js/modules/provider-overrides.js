(function(global) {
    function dashboardProviderOverridesModule() {
        return {
            providerOverridesAvailable: true,
            providerOverrideViews: [],
            providerOverrideError: '',
            providerOverrideNotice: '',
            providerOverrideSubmitting: false,
            providerOverrideToggling: {},
            providerOverrideDeleteConfirm: null,

            async fetchProviderOverrides() {
                this.providerOverrideError = '';
                try {
                    const request = typeof this.adminRequestOptions === 'function'
                        ? this.adminRequestOptions()
                        : this.requestOptions();
                    const res = await fetch('/admin/provider-overrides', request);
                    if (res.status === 503) {
                        this.providerOverridesAvailable = false;
                        this.providerOverrideViews = [];
                        return;
                    }
                    const handled = this.handleFetchResponse(res, 'provider overrides', request);
                    if (typeof this.isStaleAuthFetchResult === 'function' && this.isStaleAuthFetchResult(handled)) {
                        return;
                    }
                    this.providerOverridesAvailable = true;
                    if (!handled) {
                        this.providerOverrideViews = [];
                        return;
                    }
                    const payload = await res.json();
                    this.providerOverrideViews = Array.isArray(payload) ? payload : [];
                } catch (e) {
                    console.error('Failed to fetch provider overrides:', e);
                    this.providerOverrideViews = [];
                    this.providerOverrideError = 'Unable to load provider overrides.';
                }
            },

            providerOverrideMap() {
                const out = new Map();
                for (const override of this.providerOverrideViews) {
                    const name = String(override && override.provider_name || '').trim();
                    if (name) {
                        out.set(name, override);
                    }
                }
                return out;
            },

            getProviderOverride(providerName) {
                const name = String(providerName || '').trim();
                return this.providerOverrideMap().get(name) || null;
            },

            isProviderExplicitlyEnabled(providerName) {
                const override = this.getProviderOverride(providerName);
                if (!override) {
                    return null;
                }
                return override.enabled;
            },

            isProviderEnabled(providerName) {
                const enabled = this.isProviderExplicitlyEnabled(providerName);
                return enabled !== false;
            },

            isProviderDisabled(providerName) {
                return !this.isProviderEnabled(providerName);
            },

            providerEnabledStatusClass(providerName) {
                if (this.isProviderDisabled(providerName)) {
                    return 'is-disabled';
                }
                return 'is-enabled';
            },

            providerEnabledStatusLabel(providerName) {
                if (this.isProviderDisabled(providerName)) {
                    return 'Disabled';
                }
                return 'Enabled';
            },

            async toggleProviderEnabled(providerName) {
                const name = String(providerName || '').trim();
                if (!name) return;

                this.providerOverrideToggling[name] = true;
                this.providerOverrideError = '';
                this.providerOverrideNotice = '';

                const isCurrentlyEnabled = this.isProviderEnabled(name);
                const newEnabledState = !isCurrentlyEnabled;

                try {
                    const requestPayload = { provider_name: name, enabled: newEnabledState };
                    const request = typeof this.adminRequestOptions === 'function'
                        ? this.adminRequestOptions({ method: 'PUT', body: JSON.stringify(requestPayload) })
                        : this.requestOptions({ method: 'PUT', body: JSON.stringify(requestPayload) });
                    const res = await fetch('/admin/provider-overrides', request);
                    if (res.status === 503) {
                        this.providerOverridesAvailable = false;
                        this.providerOverrideError = 'Provider overrides feature is unavailable.';
                        return;
                    }
                    const handled = this.handleFetchResponse(res, 'provider override', request);
                    if (typeof this.isStaleAuthFetchResult === 'function' && this.isStaleAuthFetchResult(handled)) {
                        return;
                    }
                    if (!handled) {
                        this.providerOverrideError = res.status === 401
                            ? 'Authentication required.'
                            : await this.aliasResponseMessage(res, 'Failed to toggle provider state.');
                        return;
                    }
                    this.providerOverridesAvailable = true;
                    await this.fetchProviderOverrides();
                    if (typeof this.fetchProviderStatus === 'function') {
                        this.fetchProviderStatus();
                    }
                    this.providerOverrideNotice = newEnabledState
                        ? 'Provider "' + name + '" has been enabled.'
                        : 'Provider "' + name + '" has been disabled.';
                } catch (e) {
                    console.error('Failed to toggle provider enabled state:', e);
                    this.providerOverrideError = 'Failed to toggle provider state.';
                } finally {
                    this.providerOverrideToggling[name] = false;
                }
            },

            isProviderToggling(providerName) {
                const name = String(providerName || '').trim();
                return Boolean(this.providerOverrideToggling[name]);
            },

            // Override filteredDisplayModelGroups to inject provider disabled state
            get filteredDisplayModelGroups() {
                const groups = typeof this.groupDisplayModels === 'function'
                    ? this.groupDisplayModels(this.filteredDisplayModels)
                    : [];
                return groups.map((group) => {
                    const providerName = String(group.provider_name || '').trim();
                    const isProviderDisabled = providerName && this.isProviderDisabled(providerName);
                    if (isProviderDisabled) {
                        const updatedGroup = {
                            ...group,
                            access: { ...(group.access || {}), provider_disabled: true }
                        };
                        updatedGroup.rows = updatedGroup.rows.map((row) => {
                            if (row.is_alias) return row;
                            return { ...row, access: { ...(row.access || {}), provider_disabled: true } };
                        });
                        return updatedGroup;
                    }
                    return group;
                });
            },

            // Override modelAccessStateText to show Provider Disabled badge
            modelAccessStateText(access) {
                if (!this.modelOverridesAvailable) return '';
                if (!access) return 'Default';
                if (access.provider_disabled) return 'Provider Disabled';
                if (access.effective_enabled === false) {
                    return access.default_enabled === false ? 'Disabled by Default' : 'Disabled';
                }
                if (Array.isArray(access.user_paths) && access.user_paths.length > 0 && access.user_paths.indexOf('/') === -1) {
                    return 'Restricted';
                }
                return 'Enabled';
            },

            // Override modelAccessStateClass to style as disabled
            modelAccessStateClass(access) {
                if (!this.modelOverridesAvailable) return '';
                if (!access) return '';
                if (access.provider_disabled || access.effective_enabled === false) return 'is-disabled';
                if (Array.isArray(access.user_paths) && access.user_paths.length > 0 && access.user_paths.indexOf('/') === -1) {
                    return 'is-restricted';
                }
                return 'is-enabled';
            }
        };
    }

    global.dashboardProviderOverridesModule = dashboardProviderOverridesModule;
})(typeof window !== 'undefined' ? window : globalThis);