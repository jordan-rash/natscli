package cli

import (
	"fmt"
	"github.com/AlecAivazis/survey/v2"
	"github.com/choria-io/fisk"
	ab "github.com/synadia-io/jwt-auth-builder.go"
	"io"
	"os"
	"sort"
)

func (c *authAccountCommand) importAddAction(_ *fisk.ParseContext) error {
	auth, _, acct, err := c.selectAccount(true)
	if err != nil {
		return err
	}

	var imp ab.Import
	if c.isService {
		imp, err = ab.NewServiceImport(c.importName, acct.Subject(), c.subject)
	} else {
		imp, err = ab.NewStreamImport(c.importAccount, acct.Subject(), c.subject)
	}
	if err != nil {
		return fmt.Errorf("could not add import: %v", err)
	}

	err = imp.SetAccount(c.importAccount)
	if err != nil {
		return err
	}

	if c.localSubject == "" {
		err = imp.SetLocalSubject(c.subject)
	} else {
		err = imp.SetLocalSubject(c.localSubject)
	}
	if err != nil {
		return err
	}

	if c.activationToken != "" {
		err = imp.SetToken(c.activationToken)
		if err != nil {
			return err
		}
	}

	err = imp.SetShareConnectionInfo(c.share)
	if err != nil {
		return err
	}

	if c.allowTrace && !c.isService {
		err = imp.(ab.StreamImport).SetShareConnectionInfo(true)
		if err != nil {
			return err
		}
	}

	if c.isService {
		err = acct.Imports().Services().AddWithConfig(imp)
	} else {
		err = acct.Imports().Streams().AddWithConfig(imp.(ab.StreamImport))
	}
	if err != nil {
		return err
	}

	err = auth.Commit()
	if err != nil {
		return fmt.Errorf("commit failed: %v", err)
	}

	return c.fShowImport(os.Stdout, imp)
}

func (c *authAccountCommand) importLsAction(_ *fisk.ParseContext) error {
	_, _, acct, err := c.selectAccount(true)
	if err != nil {
		return err
	}

	if len(acct.Imports().Services().List()) == 0 && len(acct.Imports().Streams().List()) == 0 {
		fmt.Println("No Imports defined")
		return nil
	}

	imports := c.importsBySubject(acct)

	tbl := newTableWriter("Imports for account %s", acct.Name())
	tbl.AddHeaders("Name", "Kind", "Local Subject", "Remote Subject", "Allows Tracing", "Sharing Connection Info")

	for _, i := range imports {
		ls := i.Subject()
		if i.LocalSubject() != "" {
			ls = i.LocalSubject()
		}

		switch imp := i.(type) {
		case ab.StreamImport:
			tbl.AddRow(imp.Name(), "Stream", ls, imp.Subject(), imp.AllowTracing(), imp.IsShareConnectionInfo())
		case ab.ServiceImport:
			tbl.AddRow(imp.Name(), "Service", ls, imp.Subject(), "", imp.IsShareConnectionInfo())
		}
	}

	fmt.Println(tbl.Render())

	return nil
}

func (c *authAccountCommand) findImport(account ab.Account, localSubject string) ab.Import {
	for _, imp := range account.Imports().Streams().List() {
		if imp.LocalSubject() == localSubject {
			return imp
		}
	}
	for _, imp := range account.Imports().Services().List() {
		if imp.LocalSubject() == localSubject {
			return imp
		}
	}

	return nil
}

func (c *authAccountCommand) importsBySubject(acct ab.Account) []ab.Import {
	var ret []ab.Import

	for _, svc := range acct.Imports().Streams().List() {
		ret = append(ret, svc)
	}
	for _, svc := range acct.Imports().Services().List() {
		ret = append(ret, svc)
	}

	sort.Slice(ret, func(i, j int) bool {
		return ret[i].Subject() < ret[j].Subject()
	})

	return ret
}

func (c *authAccountCommand) importInfoAction(_ *fisk.ParseContext) error {
	_, _, acct, err := c.selectAccount(true)
	if err != nil {
		return err
	}

	if c.subject == "" {
		known := c.importSubjects(acct.Imports())

		if len(known) == 0 {
			return fmt.Errorf("no imports defined")
		}

		err = askOne(&survey.Select{
			Message:  "Select an Import",
			Options:  known,
			PageSize: selectPageSize(len(known)),
		}, &c.subject)
		if err != nil {
			return err
		}
	}

	if c.subject == "" {
		return fmt.Errorf("subject is required")
	}

	imp := c.findImport(acct, c.subject)
	if imp == nil {
		return fmt.Errorf("unknown import")
	}

	return c.fShowImport(os.Stdout, imp)
}

func (c *authAccountCommand) importEditAction(_ *fisk.ParseContext) error {
	auth, _, acct, err := c.selectAccount(true)
	if err != nil {
		return err
	}

	imp := c.findImport(acct, c.subject)
	if imp == nil {
		return fmt.Errorf("import for local subject %q not found", c.subject)
	}

	streamImport, isStream := imp.(ab.StreamImport)
	if c.allowTraceIsSet {
		if !isStream {
			return fmt.Errorf("service imports cannot allow tracing")
		}

		err = streamImport.SetAllowTracing(c.allowTrace)
		if err != nil {
			return err
		}
	}

	if c.shareIsSet {
		err = imp.SetShareConnectionInfo(c.share)
		if err != nil {
			return err
		}
	}

	if c.localSubject != "" {
		err = imp.SetLocalSubject(c.localSubject)
		if err != nil {
			return err
		}
	}

	err = auth.Commit()
	if err != nil {
		return err
	}

	return c.fShowImport(os.Stdout, imp)
}

func (c *authAccountCommand) importRmAction(_ *fisk.ParseContext) error {
	auth, _, acct, err := c.selectAccount(true)
	if err != nil {
		return err
	}

	imp := c.findImport(acct, c.subject)
	if imp == nil {
		return fmt.Errorf("subject %q is not imported", c.subject)
	}

	if !c.force {
		ok, err := askConfirmation(fmt.Sprintf("Really remove the %s Import", imp.LocalSubject()), false)
		if err != nil {
			return err
		}

		if !ok {
			return nil
		}
	}

	switch imp.(type) {
	case ab.StreamImport:
		_, err = acct.Imports().Streams().Delete(c.subject)
		fmt.Printf("Removing Stream Import for local Subject %q imported from Account %q\n", imp.LocalSubject(), imp.Account())
	case ab.ServiceImport:
		_, err = acct.Imports().Services().Delete(c.subject)
		fmt.Printf("Removing Service Import for local subject %q imported from Account %q\n", imp.LocalSubject(), imp.Account())
	}
	if err != nil {
		return err
	}

	return auth.Commit()
}

func (c *authAccountCommand) importSubjects(imports ab.Imports) []string {
	var known []string
	for _, exp := range imports.Services().List() {
		known = append(known, exp.LocalSubject())
	}
	for _, exp := range imports.Streams().List() {
		known = append(known, exp.LocalSubject())
	}

	sort.Strings(known)

	return known
}

func (c *authAccountCommand) fShowImport(w io.Writer, exp ab.Import) error {
	out, err := c.showImport(exp)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintln(w, out)
	return err
}

func (c *authAccountCommand) showImport(imp ab.Import) (string, error) {
	cols := newColumns("Import info for %s importing %s", imp.Name(), imp.LocalSubject())

	cols.AddSectionTitle("Configuration")
	cols.AddRow("Name", imp.Name())
	cols.AddRow("Local Subject", imp.LocalSubject())
	cols.AddRow("Account", imp.Account())
	cols.AddRow("Remote Subject", imp.Subject())
	cols.AddRow("Sharing Connection Info", imp.IsShareConnectionInfo())

	strImport, ok := imp.(ab.StreamImport)
	if ok {
		cols.AddRow("Allows Message Tracing", strImport.AllowTracing())
	}

	return cols.Render()
}