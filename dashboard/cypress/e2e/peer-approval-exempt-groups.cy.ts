// Peer approval for third parties: Settings → Authentication lets the
// admin pick groups whose peers skip approval, saved as
// peer_approval_exempt_groups. Needs the dev stack (`make dev.dashboard`).
// The spec toggles one existing group and then restores both the group
// list and the approval toggle it found.

const sorted = (ids: string[] = []) => [...ids].sort();

describe("Peer approval exempt groups", () => {
  beforeEach(() => {
    cy.login();
  });

  it("saves existing groups that skip approval without creating any", () => {
    const missing = `cy-missing-${Date.now()}`;
    cy.intercept("GET", "/api/accounts").as("accounts");
    cy.intercept("POST", "/api/groups").as("createGroup");
    cy.intercept("PUT", "/api/accounts/*").as("save");
    cy.visit("/settings/authentication");

    cy.wait("@accounts").then(({ response }) => {
      const initial = sorted(
        response?.body?.[0]?.settings?.extra?.peer_approval_exempt_groups,
      );

      // The settings page waits on several API calls before it renders.
      cy.get("[data-cy=peer-approval-toggle]", { timeout: 20000 })
        .invoke("attr", "aria-checked")
        .then((wasOn) => {
          if (wasOn !== "true")
            cy.get("[data-cy=peer-approval-toggle]").click();
          cy.contains("Groups that skip approval").should("be.visible");

          // Typing an unknown name offers no group to create.
          cy.get("[data-cy=group-selector-open-close]").first().click();
          cy.get("[data-cy=group-search-input]").type(missing);
          cy.get("[role=listbox]").should("not.contain", missing);
          cy.get("[data-cy=group-search-input]").clear();

          cy.get("[role=listbox] [cmdk-item]")
            .first()
            .invoke("attr", "data-value")
            .then((value) => {
              const item = `[role=listbox] [cmdk-item][data-value="${value}"]`;
              cy.get(item).click();
              cy.get("body").type("{esc}");

              cy.get("[data-cy=save-authentication-settings]")
                .should("not.be.disabled")
                .click();
              cy.wait("@save").then(({ request, response: saved }) => {
                expect(saved?.statusCode).to.eq(200);
                const extra = request.body.settings.extra;
                expect(extra.peer_approval_enabled).to.eq(true);
                const ids = sorted(extra.peer_approval_exempt_groups);
                const changed = ids
                  .filter((id) => !initial.includes(id))
                  .concat(initial.filter((id) => !ids.includes(id)));
                expect(changed, "exactly one group toggled").to.have.length(1);
              });

              // A reload shows a clean form.
              cy.reload();
              cy.get("[data-cy=save-authentication-settings]", {
                timeout: 20000,
              }).should("be.disabled");

              // Restore: toggle the same group back and the switch.
              cy.get("[data-cy=group-selector-open-close]").first().click();
              cy.get(item).click();
              cy.get("body").type("{esc}");
              if (wasOn !== "true")
                cy.get("[data-cy=peer-approval-toggle]").click();
              cy.get("[data-cy=save-authentication-settings]").click();
              cy.wait("@save").then(({ request }) => {
                expect(
                  sorted(request.body.settings.extra.peer_approval_exempt_groups),
                ).to.deep.eq(initial);
              });

              cy.get("@createGroup.all").should("have.length", 0);
            });
        });
    });
  });
});
