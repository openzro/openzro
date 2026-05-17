// openZro #5 Q2 — behaviour contract for the client self-update
// directive form (ClientUpdateSettingsCard) on /settings (Clients
// tab). Like every dashboard E2E this needs the running stack
// (`make test.dashboard`); it asserts that editing the target version
// and saving issues a PUT /api/accounts/{id} whose settings body
// carries the client_update_* fields.

describe("Client update directive", () => {
  it("persists the directive via PUT /accounts", () => {
    cy.intercept("PUT", "/api/accounts/*").as("saveAccount");

    cy.visit("/settings?tab=clients");

    cy.get('[data-cy=client-update-version]').clear().type("0.40.0");
    cy.get('[data-cy=client-update-save]').click();

    cy.wait("@saveAccount").then(({ request }) => {
      expect(request.body).to.have.nested.property(
        "settings.client_update_target_version",
        "0.40.0",
      );
      expect(request.body.settings).to.have.property("client_update_force");
      expect(request.body.settings).to.have.property(
        "client_update_target_groups",
      );
      expect(request.body.settings).to.have.property(
        "client_update_target_peers",
      );
      expect(request.body.settings).to.have.property(
        "client_update_exclude_groups",
      );
    });
  });
});
