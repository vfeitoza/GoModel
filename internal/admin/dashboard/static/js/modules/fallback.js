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
    isDeleting: {},
    draggedIndex: null,

    init() {
      this.fetchRules();
      this.loadAvailableModels();
    },

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
      // Get all models from the existing modelsData
      if (!this.modelsData || !this.modelsData.models) {
        this.availableModels = [];
        return;
      }

      const models = [];
      for (const model of this.modelsData.models) {
        // Add qualified model (provider/model)
        if (model.provider_name && model.id) {
          models.push({
            id: `${model.provider_name}/${model.id}`,
            display: `${model.provider_name}/${model.id}`,
            provider: model.provider_name,
          });
        }
        // Also add bare model ID if not already qualified
        if (model.id && !model.id.includes("/")) {
          models.push({
            id: model.id,
            display: model.id,
            provider: model.provider_name || "",
          });
        }
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
      this.editingRule = {
        source_model: "",
        fallback_models: [],
      };
      this.newSourceModel = "";
      this.selectedModels = [];
      this.searchTerm = "";
    },

    editRule(rule) {
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
          const error = await response.json();
          throw new Error(error.error || "Failed to save fallback rule");
        }

        await this.fetchRules();
        this.cancelEdit();
        this.showSuccess("Fallback rule saved successfully");
      } catch (error) {
        console.error("Failed to save fallback rule:", error);
        this.showError(error.message || "Failed to save fallback rule");
      } finally {
        this.isSaving = false;
      }
    },

    async deleteRule(sourceModel) {
      if (!confirm(`Delete fallback rule for "${sourceModel}"?`)) return;

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
        this.showError(error.message || "Failed to delete fallback rule");
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
  };
}
