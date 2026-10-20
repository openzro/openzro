// Peer approval for third parties: Settings → Authentication lets the
// admin pick groups whose peers skip approval, saved as
// peer_approval_exempt_groups. Needs the dev stack (`make dev.dashboard`).
// The spec removes the group it adds and restores the toggle it found.

describe("Peer approval exempt groups", () => {
  beforeEach(() => {
    cy.login();
  });

  it("saves the groups that skip approval", () => {
    const group = `cy-staff-${Date.now()}`;
    cy.intercept("PUT", "/api/accounts/*").as("save");
    cy.visit("/settings/authentication");

    // The settings page waits on several API calls before it renders.
    cy.get("[data-cy=peer-approval-toggle]", { timeout: 20000 })
      .invoke("attr", "aria-checked")
      .then((wasOn) => {
        if (wasOn !== "true") cy.get("[data-cy=peer-approval-toggle]").click();
        cy.contains("Groups that skip approval").should("be.visible");

        cy.get("[data-cy=group-selector-open-close]").first().click();
        cy.get("[data-cy=group-search-input]").type(`${group}{enter}`);
        cy.get("body").type("{esc}");

        cy.get("[data-cy=save-authentication-settings]")
          .should("not.be.disabled")
          .click();
        cy.wait("@save").then(({ request, response }) => {
          expect(response?.statusCode).to.eq(200);
          expect(request.body.settings.extra.peer_approval_enabled).to.eq(true);
          const ids: string[] =
            request.body.settings.extra.peer_approval_exempt_groups;
          expect(ids).to.have.length.greaterThan(0);
          cy.wrap(ids.length).as("savedCount");
        });

        // A reload shows the saved group and a clean form.
        cy.reload();
        cy.contains(group).should("be.visible");
        cy.get("[data-cy=save-authentication-settings]").should("be.disabled");

        // Restore: remove the group and put the toggle back.
        cy.get("[data-cy=group-selector-open-close]").first().click();
        cy.get("[role=listbox] [cmdk-item]").contains(group).click();
        cy.get("body").type("{esc}");
        if (wasOn !== "true") cy.get("[data-cy=peer-approval-toggle]").click();
        cy.get("[data-cy=save-authentication-settings]").click();
        cy.get<number>("@savedCount").then((savedCount) => {
          cy.wait("@save")
            .its("request.body.settings.extra.peer_approval_exempt_groups")
            .should("have.length", savedCount - 1);
        });
      });
  });
});
