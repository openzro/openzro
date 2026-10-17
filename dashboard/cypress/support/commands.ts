/// <reference types="cypress" />
// ***********************************************
// This example commands.ts shows you how to
// create various custom commands and overwrite
// existing commands.
//
// For more comprehensive examples of custom
// commands please read more here:
// https://on.cypress.io/custom-commands
// ***********************************************
//
//
// -- This is a parent command --
// Cypress.Commands.add('login', (email, password) => { ... })
//
//
// -- This is a child command --
// Cypress.Commands.add('drag', { prevSubject: 'element'}, (subject, options) => { ... })
//
//
// -- This is a dual command --
// Cypress.Commands.add('dismiss', { prevSubject: 'optional'}, (subject, options) => { ... })
//
//
// -- This will overwrite an existing command --
// Cypress.Commands.overwrite('visit', (originalFn, url, options) => { ... })
//
// declare global {
//   namespace Cypress {
//     interface Chainable {
//       login(email: string, password: string): Chainable<void>
//       drag(subject: string, options?: Partial<TypeOptions>): Chainable<Element>
//       dismiss(subject: string, options?: Partial<TypeOptions>): Chainable<Element>
//       visit(originalFn: CommandOriginalFn, url: string, options: Partial<VisitOptions>): Chainable<Element>
//     }
//   }
// }
// login signs in through the local dev Dex (`make dev.idp.up`) and caches
// the session, so specs that need an authenticated dashboard run against
// the real stack. Defaults to the seeded dev owner.
Cypress.Commands.add(
  "login",
  (email = "admin@openzro.dev", password = "openzro") => {
    cy.session([email], () => {
      cy.visit("/peers");
      cy.origin(
        "http://localhost:5556",
        { args: { email, password } },
        ({ email, password }) => {
          cy.get("input[name=login]").type(email);
          cy.get("input[name=password]").type(password, { log: false });
          cy.get("button[type=submit]").click();
        },
      );
      cy.location("origin").should("eq", "http://localhost:3000");
      cy.contains("Peers");
    });
  },
);

declare global {
  // eslint-disable-next-line @typescript-eslint/no-namespace
  namespace Cypress {
    interface Chainable {
      login(email?: string, password?: string): Chainable<void>;
    }
  }
}

export {};
