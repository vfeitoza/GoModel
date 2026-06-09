/**
 * Dashboard fallback management module
 */
function dashboardFallbackModule() {
  return {
    rules: [],
    editingRule: null,
    newSourceModel: "",
    selectedModels: [],
    availableModels: [],
    searchTerm: "",
    isSaving: false,
    isRefreshing: false,
    isDeleting: {},
    draggedIndex: null,

    async fetchRules() {
      try {
        const request = typeof this.requestOptions === 'function' ? this.requestOptions() : { headers: this.headers() };
        const response = await fetch("/admin/fallback/rules", request);

        if (!response.ok) {
          throw new Error("Failed to fetch fallback rules");
        }

        const data = await response.json();
        this.rules = data.rules || [];
      } catch (error) {
        console.error("Failed to fetch fallback rules:", error);
        this.showError("Failed to load fallback rules");
      }
    },

    loadAvailableModels() {
      // Get all models from the existing models array
      if (!this.models || !Array.isArray(this.models)) {
        console.warn('fallback: models not available', this.models);
        this.availableModels = [];
        return;
      }

      console.log('fallback: loading models from', this.models.length, 'models');
      console.log('fallback: first model structure:', this.models[0]);

      const models = [];
      for (const model of this.models) {
        // Extract model ID from the nested structure
        let modelId = '';

        if (model.model && typeof model.model.id === 'string') {
          modelId = model.model.id;
        } else if (typeof model.model === 'string') {
          modelId = model.model;
        } else if (typeof model.id === 'string') {
          modelId = model.id;
        }

        if (!modelId || !model.provider_name) {
          continue; // Skip models without ID or provider
        }

        // Only add qualified model (provider/model)
        models.push({
          id: `${model.provider_name}/${modelId}`,
          display: `${model.provider_name}/${modelId}`,
          provider: model.provider_name,
        });
      }

      // Remove duplicates
      const seen = new Set();
      this.availableModels = models.filter((m) => {
        if (seen.has(m.id)) return false;
        seen.add(m.id);
        return true;
      });

      // Sort by display name
      this.availableModels.sort((a, b) => a.display.localeCompare(b.display));

      console.log('fallback: loaded', this.availableModels.length, 'unique models');
      console.log('fallback: first 5 models:', this.availableModels.slice(0, 5));
    },

    get filteredAvailableModels() {
      const term = this.searchTerm.toLowerCase().trim();
      if (!term) return this.availableModels;
      return this.availableModels.filter(
        (m) =>
          m.display.toLowerCase().includes(term) ||
          m.provider.toLowerCase().includes(term)
      );
    },

    startNewRule() {
      console.log('fallback: startNewRule called');
      this.loadAvailableModels(); // Ensure models are loaded
      this.editingRule = {
        source_model: "",
        fallback_models: [],
      };
      this.newSourceModel = "";
      this.selectedModels = [];
      this.searchTerm = "";
      console.log('fallback: editingRule set to', this.editingRule);
      console.log('fallback: availableModels', this.availableModels.length);
    },

    editRule(rule) {
      this.loadAvailableModels(); // Ensure models are loaded
      this.editingRule = {
        source_model: rule.source_model,
        fallback_models: [...rule.fallback_models],
      };
      this.newSourceModel = rule.source_model;
      this.selectedModels = [...rule.fallback_models];
      this.searchTerm = "";
    },

    cancelEdit() {
      this.editingRule = null;
      this.newSourceModel = "";
      this.selectedModels = [];
      this.searchTerm = "";
    },

    addModel(modelId) {
      if (!this.selectedModels.includes(modelId)) {
        this.selectedModels.push(modelId);
      }
      this.searchTerm = "";
    },

    removeModel(index) {
      this.selectedModels.splice(index, 1);
    },

    // Drag and drop handlers
    dragStart(index) {
      this.draggedIndex = index;
    },

    dragOver(event, index) {
      event.preventDefault();
      if (this.draggedIndex === null || this.draggedIndex === index) return;

      const draggedItem = this.selectedModels[this.draggedIndex];
      this.selectedModels.splice(this.draggedIndex, 1);
      this.selectedModels.splice(index, 0, draggedItem);
      this.draggedIndex = index;
    },

    dragEnd() {
      this.draggedIndex = null;
    },

    async saveRule() {
      const sourceModel = this.newSourceModel.trim();
      if (!sourceModel) {
        this.showError("Source model is required");
        return;
      }

      if (this.selectedModels.length === 0) {
        this.showError("At least one fallback model is required");
        return;
      }

      console.log('fallback: saving rule', {
        source_model: sourceModel,
        fallback_models: this.selectedModels
      });

      this.isSaving = true;
      try {
        const request = typeof this.requestOptions === 'function' ? this.requestOptions() : { headers: this.headers() };
        const response = await fetch("/admin/fallback/rules", {
          method: "PUT",
          headers: {
            ...request.headers,
            "Content-Type": "application/json",
          },
          body: JSON.stringify({
            source_model: sourceModel,
            fallback_models: this.selectedModels,
          }),
        });

        if (!response.ok) {
          const contentType = response.headers.get("content-type");
          let errorMessage = `Failed to save fallback rule (${response.status})`;

          try {
            if (contentType && contentType.includes("application/json")) {
              const error = await response.json();
              console.error('fallback: server error response:', JSON.stringify(error, null, 2));

              // Extract nested error message
              if (error.error && typeof error.error === 'object') {
                errorMessage = error.error.error || error.error.message || JSON.stringify(error.error);
              } else if (error.error && typeof error.error === 'string') {
                errorMessage = error.error;
              } else if (error.message) {
                errorMessage = error.message;
              } else {
                errorMessage = JSON.stringify(error);
              }
            } else {
              const text = await response.text();
              console.error('fallback: server error text:', text);
              errorMessage = text || errorMessage;
            }
          } catch (parseError) {
            console.error('fallback: failed to parse error:', parseError);
          }

          throw new Error(errorMessage);
        }

        await this.fetchRules();
        this.cancelEdit();
        this.showSuccess("Fallback rule saved successfully");
      } catch (error) {
        console.error("Failed to save fallback rule:", error);
        const errorMsg = error && error.message ? error.message : String(error);
        this.showError(errorMsg || "Failed to save fallback rule");
      } finally {
        this.isSaving = false;
      }
    },

    async deleteRule(sourceModel) {
      console.log('fallback: deleteRule called for', sourceModel);
      if (!confirm(`Delete fallback rule for "${sourceModel}"?`)) {
        console.log('fallback: delete cancelled');
        return;
      }

      console.log('fallback: deleting rule...');
      this.isDeleting[sourceModel] = true;
      try {
        const request = typeof this.requestOptions === 'function' ? this.requestOptions() : { headers: this.headers() };
        const response = await fetch(
          `/admin/fallback/rules/${encodeURIComponent(sourceModel)}`,
          {
            method: "DELETE",
            headers: request.headers,
          }
        );

        if (!response.ok) {
          const error = await response.json();
          throw new Error(error.error || "Failed to delete fallback rule");
        }

        await this.fetchRules();
        this.showSuccess("Fallback rule deleted successfully");
      } catch (error) {
        console.error("Failed to delete fallback rule:", error);
        const errorMsg = error && error.message ? error.message : String(error);
        this.showError(errorMsg || "Failed to delete fallback rule");
      } finally {
        delete this.isDeleting[sourceModel];
      }
    },

    showSuccess(message) {
      // Use existing toast/notification system if available
      console.log("Success:", message);
    },

    showError(message) {
      // Use existing toast/notification system if available
      console.error("Error:", message);
      alert(message);
    },

    async refreshRuntime() {
      console.log('fallback: refreshRuntime called');
      this.isRefreshing = true;

      try {
        const request = typeof this.requestOptions === 'function' ? this.requestOptions() : { headers: this.headers() };
        const response = await fetch("/admin/runtime/refresh", {
          method: "POST",
          headers: request.headers,
        });

        if (!response.ok) {
          const contentType = response.headers.get("content-type");
          let errorMessage = `Failed to refresh runtime (${response.status})`;

          try {
            if (contentType && contentType.includes("application/json")) {
              const error = await response.json();
              console.error('fallback: refresh error response:', JSON.stringify(error, null, 2));
              errorMessage = error.error?.message || error.error || error.message || JSON.stringify(error);
            } else {
              const text = await response.text();
              console.error('fallback: refresh error text:', text);
              errorMessage = text || errorMessage;
            }
          } catch (parseError) {
            console.error('fallback: failed to parse refresh error:', parseError);
          }

          throw new Error(errorMessage);
        }

        const result = await response.json();
        console.log('fallback: runtime refreshed', result);

        // Reload the rules from the server
        await this.fetchRules();

        this.showSuccess("Configuration reloaded successfully");
      } catch (error) {
        console.error("Failed to refresh runtime:", error);
        const errorMsg = error && error.message ? error.message : String(error);
        this.showError(errorMsg || "Failed to refresh runtime");
      } finally {
        this.isRefreshing = false;
      }
    },
  };
}
