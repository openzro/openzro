// ADR-0026 D3 — console management of local users. Needs the dev stack
// with creation enabled:
//   OPENZRO_SCIM_LOCAL_USERS_ENABLED=true make dev.management.up
// Each run creates a new address, because a deactivated local user's
// email can't be reused.

const password = "correct-horse-battery";

describe("Local users", () => {
  beforeEach(() => {
    cy.login();
  });

  it("creates, resets and deactivates a local user", () => {
    const email = `cy-${Date.now()}@example.com`;
    cy.intercept("POST", "/api/users/local").as("create");
    cy.intercept("POST", "/api/users/*/password").as("reset");
    cy.intercept("POST", "/api/users/*/deactivate").as("deactivate");

    cy.visit("/team/users");
    cy.get("[data-cy=create-local-user]").click();
    cy.get("[data-cy=local-user-email]").type(email);
    cy.get("[data-cy=local-user-password]").type("too-short");
    cy.get("[data-cy=local-user-password-confirm]").type("too-short");
    cy.get("[data-cy=local-user-create-submit]").should("be.disabled");

    cy.get("[data-cy=local-user-password]").clear().type(password);
    cy.get("[data-cy=local-user-password-confirm]").clear().type(password);
    cy.get("[data-cy=local-user-create-submit]").click();
    cy.wait("@create").then(({ request, response }) => {
      expect(request.body).to.include({ email, password });
      expect(response?.statusCode).to.eq(200);
      expect(JSON.stringify(response?.body)).not.to.contain(password);
    });

    const row = () => cy.contains("tr", email);
    row().find("[data-cy=local-user-badge]").should("contain", "Local");
    row().find("[role=switch]").should("not.exist");

    row().find("[aria-label='Row actions']").click();
    cy.get("[data-cy=reset-local-password]").click();
    cy.get("[data-cy=local-user-password]").type(`${password}-2`);
    cy.get("[data-cy=local-user-password-confirm]").type(`${password}-2`);
    cy.get("[data-cy=local-user-reset-submit]").click();
    cy.wait("@reset").its("response.statusCode").should("eq", 200);
    cy.location("pathname").should("eq", "/team/users");

    row().find("[aria-label='Row actions']").click();
    cy.get("[data-cy=deactivate-local-user]").click();
    cy.contains("button", "Deactivate").click();
    cy.wait("@deactivate").its("response.statusCode").should("eq", 200);
    row()
      .find("[data-cy=local-user-badge]")
      .should("contain", "Local · deactivated");

    row().find("[aria-label='Row actions']").click();
    cy.get("[data-cy=reset-local-password]").should("not.exist");
    cy.get("[data-cy=deactivate-local-user]").should("not.exist");
  });
});
